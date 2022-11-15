package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

var verboseOutput bool
var jsonLog bool
var testMode bool
var dryRun bool

const (
	envNameVerbose string = "FLOATY_LOG_VERBOSE"
)

func init() {
	flag.BoolVar(&verboseOutput, "v", false, "")
	flag.BoolVar(&verboseOutput, "verbose", false,
		fmt.Sprintf("Verbose logging (environment variable: %s)",
			envNameVerbose))

	flag.BoolVar(&jsonLog, "json-log", false, "Log output in JSON format")
	flag.BoolVar(&dryRun, "dry-run", false, "Don't make calls to a cloud provider")

	for _, i := range []string{"T", "test"} {
		flag.BoolVar(&testMode, i, false,
			"Test mode; verify configuration and API access")
	}
}

const flagUsage = "{ -T <config-path> | <config-path> [group|instance] <vrrp-name> <vrrp-status> <priority> }"

type notifyProgram struct {
	config            notifyConfig
	addresses         []netAddress
	keepalivedProcess *keepalivedProcess
}

func (p notifyProgram) notifyNoop(ctx context.Context) {
}

func (p notifyProgram) notifyMaster(ctx context.Context) {
	provider, err := p.config.NewProvider()
	if err != nil {
		logrus.Fatal(err)
	}

	refreshers := []elasticIPRefresher{}

	for _, address := range p.addresses {
		logger := logrus.WithField("address", address)
		refresher, err := provider.NewElasticIPRefresher(logger, address)
		if err != nil {
			logrus.Fatal(err)
		}
		refreshers = append(refreshers, refresher)
	}

	wg := sync.WaitGroup{}
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
	return verboseOutput || (len(os.Getenv(envNameVerbose)) > 0)
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

func loadConfig(path string) (notifyConfig, error) {
	cfg := newNotifyConfig()

	if err := cfg.ReadFromYAML(path); err != nil {
		return cfg, err
	}

	logrus.WithField("config", cfg).Debugf("Configuration")

	return cfg, nil
}

func providerTest(path string) error {
	logrus.Info("Running self-test")

	cfg, err := loadConfig(path)
	if err != nil {
		return err
	}

	provider, err := cfg.NewProvider()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)

	defer cancel()

	return provider.Test(ctx)
}

func main() {
	var err error
	ctx := context.Background()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	logrus.SetOutput(os.Stderr)
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	flag.Usage = func() {
		version := newVersionInfo().HumanReadable()

		fmt.Fprintf(flag.CommandLine.Output(),
			"Usage: %s %s\n\nVersion: %s\n\nOptions:\n",
			os.Args[0], flagUsage, version)
		flag.PrintDefaults()
	}
	flag.Parse()

	if !useVerboseLogging() {
		logrus.SetLevel(logrus.InfoLevel)
	}

	if jsonLog {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	}

	switch {
	case testMode && flag.NArg() == 1:
	case flag.NArg() == 5:
	default:
		flag.Usage()
		os.Exit(2)
	}

	configFile := flag.Arg(0)

	if err = configOutOfMemoryKiller(); err != nil {
		log.Fatal(err)
	}

	if testMode {
		if err := providerTest(configFile); err != nil {
			log.Fatal(err)
		}
		return
	}

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
		"version":       newVersionInfo().HumanReadable(),
	}).Info("Hello world")

	p := notifyProgram{}

	if p.config, err = loadConfig(configFile); err != nil {
		log.Fatal(err)
	}

	if dryRun {
		p.config.Provider = "fake"
	}

	p.addresses, err =
		readAddressesFromKeepalivedConfig(p.config.KeepalivedConfigFile, vrrpInstanceName)
	if err != nil {
		logrus.Fatal(err)
	}

	if len(p.config.ManagedAddresses) > 0 {
		p.addresses = p.config.ManagedAddresses
	}

	logrus.WithField("addresses", p.addresses).Infof("IP addresses")

	// Keepalived does not terminate long-running notification programs when
	// exiting. In addition Keepalived may be terminated through other means
	// such as SIGKILL. In such cases the IP address updates must stop as soon
	// as possible. As of Keepalived 1.2, shipped with OpenShift 3.9, there is
	// no mechanism to reliably detect that Keepalived has terminated. Later
	// versions have support for FIFOs to communicate to notification programs.
	// Therefore the only reasonable method is to locate the process ID of
	// Keepalived and polling for its validity in a regular interval.
	if p.keepalivedProcess, err = findKeepalivedProcessParent(); err != nil {
		logrus.Warningf("Keepalived not found: %s", err)
		p.keepalivedProcess = nil
	} else {
		go p.keepalivedProcess.waitForTermination(ctx, stop)
	}
	statusFunc := map[string]func(context.Context){
		"fault":  p.notifyNoop,
		"master": p.notifyMaster,
		"backup": p.notifyNoop,
	}

	fn, ok := statusFunc[strings.ToLower(vrrpStatus)]
	if !ok {
		logrus.Fatalf("Unrecognized VRRP status %q", vrrpStatus)
	}

	unlock, err := acquireLock(ctx, p.config.MakeLockFilePath(vrrpInstanceName), p.config.LockTimeout)
	if err != nil {
		logrus.Fatalf("Failed to acquire lock: %s", err)
	}
	defer func() {
		if err := unlock(); err != nil {
			logrus.Errorf("Unlocking failed: %s", err)
		}
	}()

	fn(ctx)
}
