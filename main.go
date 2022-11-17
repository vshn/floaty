package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/sirupsen/logrus"
)

var verboseOutput bool
var jsonLog bool
var dryRun bool

var testMode bool
var fifoMode bool

const (
	envNameVerbose string = "FLOATY_LOG_VERBOSE"

	flagUsage = "{ -T <config-path> | <config-path> [group|instance] <vrrp-name> <vrrp-status> <priority> | --fifo <config-path> <fifo-path> }"
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

	flag.BoolVar(&fifoMode, "fifo", false, "Run in fifo mode")

	flag.Usage = func() {
		version := newVersionInfo().HumanReadable()

		fmt.Fprintf(flag.CommandLine.Output(),
			"Usage: %s %s\n\nVersion: %s\n\nOptions:\n",
			os.Args[0], flagUsage, version)
		flag.PrintDefaults()
	}
}

func setupLogger() {
	logrus.SetOutput(os.Stderr)
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	if !useVerboseLogging() {
		logrus.SetLevel(logrus.InfoLevel)
	}
	if jsonLog {
		logrus.SetFormatter(&logrus.JSONFormatter{})
	}
}

func useVerboseLogging() bool {
	return verboseOutput || (len(os.Getenv(envNameVerbose)) > 0)
}

func main() {
	var err error
	ctx := context.Background()
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}

	setupLogger()

	WaitForKeepalivedTermination(ctx, stop)
	if err = configOutOfMemoryKiller(); err != nil {
		log.Fatal(err)
	}

	configFile := flag.Arg(0)
	cfg, err := loadConfig(configFile, dryRun)
	if err != nil {
		log.Fatal(err)
	}

	switch {
	case testMode:
		err = testProvider(ctx, cfg)
	case fifoMode:
		err = runFifo(ctx, cfg)
	default:
		err = runNotify(ctx, cfg)
	}
	if err != nil {
		log.Fatal(err)
	}
}

func runFifo(ctx context.Context, cfg notifyConfig) error {
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	fifoPath := flag.Arg(1)

	// Open pipe with O_NONBLOCK to ensure we don't get stuck here and
	// miss the first write
	p, err := os.OpenFile(fifoPath, os.O_RDONLY|syscall.O_NONBLOCK, 0601)
	if os.IsNotExist(err) {
		return fmt.Errorf("Named pipe '%s' does not exist", fifoPath)
	} else if os.IsPermission(err) {
		return fmt.Errorf("Insufficient permissions to read named pipe '%s': %w", fifoPath, err)
	} else if err != nil {
		return fmt.Errorf("Error while opening named pipe '%s': %w", fifoPath, err)
	}
	defer p.Close()
	logrus.Infof("Opened file %q", fifoPath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("Failed to watch for changes in fifo: %w", err)
	}
	defer watcher.Close()
	err = watcher.Add(fifoPath)
	if err != nil {
		return fmt.Errorf("Failed to watch fifo %q: %w", fifoPath, err)
	}

	fifoHandler, err := NewFifoHandler(cfg, p, watcher.Events)
	if err != nil {
		return fmt.Errorf("Failed to setup FIFO handler: %w", err)
	}
	return fifoHandler.HandleFifo(ctx)
}

func runNotify(ctx context.Context, cfg notifyConfig) error {
	if flag.NArg() != 5 {
		flag.Usage()
		os.Exit(2)
	}

	notification, err := parseNotification(flag.Args()[1:])
	if err != nil {
		flag.Usage()
		return fmt.Errorf("Failed to parse notification: %w", err)
	}

	logrus.WithFields(logrus.Fields{
		"config-file":   flag.Arg(0),
		"instance-name": notification.Instance,
		"status":        notification.Status,
		"version":       newVersionInfo().HumanReadable(),
	}).Info("Hello world")

	// Make sure we stop any earlier scripts by acquiring the lock and killing the old process
	unlock, err := acquireLock(ctx, cfg.MakeLockFilePath(notification.Instance), cfg.LockTimeout)
	if err != nil {
		return fmt.Errorf("Failed to acquire lock: %w", err)
	}
	defer func() {
		if err := unlock(); err != nil {
			logrus.Errorf("Unlocking failed: %s", err)
		}
	}()

	provider, err := cfg.NewProvider()
	if err != nil {
		return err
	}
	return handleNotification(ctx, provider, cfg, notification)
}

func testProvider(ctx context.Context, cfg notifyConfig) error {
	logrus.Info("Running self-test")
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}

	provider, err := cfg.NewProvider()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()
	return provider.Test(ctx)
}
