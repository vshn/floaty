package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/nightlyone/lockfile"
	"github.com/sirupsen/logrus"
)

var verboseOutput bool
var jsonLog bool

func init() {
	const defaultVerbose = false

	flag.BoolVar(&verboseOutput, "v", defaultVerbose, "")
	flag.BoolVar(&verboseOutput, "verbose", defaultVerbose, "Verbose logging")

	flag.BoolVar(&jsonLog, "json-log", false, "Log output in JSON format")
}

const flagUsage = "<config-path> [group|instance] <vrrp-name> <vrrp-status> <priority>"

var commitRefName, commitSHA string

type notifyProgram struct {
	config    notifyConfig
	addresses []netAddress
	lock      lockfile.Lockfile
}

// Attempt to acquire a file-based lock or, if that isn't possible within
// a configurable amount of time, exit with an error message. If there is
// already a process owning the lock it's sent a SIGTERM signal.
func (p notifyProgram) acquireLockOrDie() {
	sentSIGTERM := false

	fn := func() error {
		err := p.lock.TryLock()

		if err == nil {
			return nil
		}

		if err == lockfile.ErrBusy && !sentSIGTERM {
			if proc, errOwner := p.lock.GetOwner(); errOwner == nil && proc != nil {
				// Tell existing process to terminate
				if errSigterm := proc.Signal(syscall.SIGTERM); errSigterm == nil {
					logrus.Debugf("Sent SIGTERM to PID %d", proc.Pid)
					sentSIGTERM = true
				} else {
					logrus.Warningf("Sending SIGTERM to PID %d failed: %s", proc.Pid, errSigterm)
				}
			}

			return err
		}

		if _, ok := err.(lockfile.TemporaryError); ok {
			// Try again
			return err
		}

		switch err {
		case lockfile.ErrInvalidPid, lockfile.ErrDeadOwner, lockfile.ErrRogueDeletion:
			// Try again
			return err
		}

		// Give up for unknown errors
		return backoff.Permanent(err)
	}

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 10 * time.Millisecond
	bo.MaxInterval = 100 * time.Millisecond
	bo.MaxElapsedTime = p.config.LockTimeout
	bo.Reset()

	if err := backoff.Retry(fn, bo); err != nil {
		logrus.Fatal(err)
	}

	if proc, err := p.lock.GetOwner(); err != nil {
		logrus.Fatalf("Getting lock owner: %s", err)
	} else if proc.Pid != os.Getpid() {
		logrus.Fatalf("Lock owned by PID %d", proc.Pid)
	}

	logrus.Debugf("Lock on file %q acquired", p.lock)
}

func (p notifyProgram) notifyNoop() {
}

func (p notifyProgram) notifyMaster() {
	provider, err := p.config.NewProvider()
	if err != nil {
		logrus.Fatal(err)
	}

	refreshers := []elasticIPRefresher{}

	for _, address := range p.addresses {
		refresher, err := provider.NewElasticIPRefresher(p.config, address)
		if err != nil {
			logrus.Fatal(err)
		}

		refreshers = append(refreshers, refresher)
	}

	ctx, cancelFunc := context.WithCancel(context.Background())

	exitSignal := make(chan os.Signal)

	signal.Notify(exitSignal, syscall.SIGINT, syscall.SIGTERM)

	// Handle signals
	go func() {
		var signum = -1

		receivedSignal := <-exitSignal

		if sysSignal, ok := receivedSignal.(syscall.Signal); ok {
			signum = int(sysSignal)
		}

		logrus.Infof("Received signal %d (%s)", signum, receivedSignal.String())

		cancelFunc()
	}()

	wg := sync.WaitGroup{}

	// Start all refreshers before waiting for them to terminate
	for _, i := range refreshers {
		wg.Add(1)

		go func(refresher elasticIPRefresher) {
			defer wg.Done()

			runRefresher(ctx, p.config, refresher)
		}(i)
	}

	wg.Wait()
}

func useVerboseLogging() bool {
	return verboseOutput || (len(os.Getenv("URSULA_LOG_VERBOSE")) > 0)
}

func readAddressesFromKeepalivedConfig(path, vrrpInstanceName string) ([]netAddress, error) {
	parsed, err := parseKeepalivedConfigFile(path)
	if err != nil {
		return nil, err
	}

	vrrpInstance, ok := parsed.vrrpInstances[vrrpInstanceName]
	if !ok {
		return nil, fmt.Errorf("No VRRP instance named %q", vrrpInstanceName)
	}

	return vrrpInstance.Addresses, nil
}

func main() {
	var err error

	logrus.SetOutput(os.Stderr)
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	flag.Usage = func() {
		version := "unknown"

		if len(commitRefName) > 0 {
			version = commitRefName
		}

		if len(commitSHA) > 0 {
			version = fmt.Sprintf("%s (commit %s)", version, commitSHA[:10])
		}

		fmt.Fprintf(flag.CommandLine.Output(),
			"Usage: %s %s\n\nVersion: %s\n\nOptions:\n",
			os.Args[0], flagUsage, version)
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() != 5 {
		flag.Usage()
		os.Exit(2)
	}

	if !useVerboseLogging() {
		logrus.SetLevel(logrus.InfoLevel)
	}

	if jsonLog {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	}

	configFile := flag.Arg(0)

	if strings.ToLower(flag.Arg(1)) != "instance" {
		// TODO: Implement group notifications
		logrus.Fatal("Only instance notifications are supported")
	}

	vrrpInstanceName := flag.Arg(2)
	vrrpStatus := flag.Arg(3)

	logrus.WithFields(logrus.Fields{
		"config-file":   configFile,
		"instance-name": vrrpInstanceName,
		"status":        vrrpStatus,
	}).Info("Hello world")

	p := notifyProgram{
		config: newNotifyConfig(),
	}

	if err = p.config.ReadFromYAML(configFile); err != nil {
		logrus.Fatal(err)
	}

	logrus.WithField("config", p.config).Debugf("Configuration")

	p.addresses, err =
		readAddressesFromKeepalivedConfig(p.config.KeepalivedConfigFile, vrrpInstanceName)
	if err != nil {
		logrus.Fatal(err)
	}

	logrus.WithField("addresses", p.addresses).Infof("IP addresses")

	{
		lockFilePath := p.config.MakeLockFilePath(vrrpInstanceName)

		p.lock, err = lockfile.New(lockFilePath)
		if err != nil {
			logrus.Fatalf("Initializing lock file %q: %s", lockFilePath, err)
		}
	}

	statusFunc := map[string]func(){
		"fault":  p.notifyNoop,
		"master": p.notifyMaster,
		"backup": p.notifyNoop,
	}

	fn, ok := statusFunc[strings.ToLower(vrrpStatus)]
	if !ok {
		logrus.Fatalf("Unrecognized VRRP status %q", vrrpStatus)
	}

	p.acquireLockOrDie()

	defer func() {
		if err := p.lock.Unlock(); err != nil {
			logrus.Errorf("Unlocking %q failed: %s", p.lock, err)
		}
	}()

	fn()
}
