package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/mitchellh/go-ps"
	"github.com/sirupsen/logrus"
)

func processMatches(proc ps.Process, exe string) bool {
	return filepath.Base(proc.Executable()) == exe
}

// findParentProcess walks the stack of parent processes and looks for the
// topmost process whose binary has the given base name.
func findParentProcess(exe string) (ps.Process, error) {
	var candidate ps.Process

	for pid := os.Getpid(); ; {
		proc, err := ps.FindProcess(pid)
		if err != nil {
			return nil, err
		}

		if proc == nil {
			break
		}

		logrus.Debugf("%#v", proc)

		if processMatches(proc, exe) {
			candidate = proc
		}

		pid = proc.PPid()
	}

	if candidate != nil {
		return candidate, nil
	}

	return nil, fmt.Errorf("Process with executable name %q not found among parents", exe)
}

// waitForProcessToTerminate waits until either the context has been cancelled
// or the process has terminated. If the process exists the name of its binary
// is compared to the given base name.
func waitForProcessToTerminate(ctx context.Context, proc *ps.UnixProcess,
	exe string) (bool, error) {

	// Arbitrary time limits to recover from temporary errors
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 100 * time.Millisecond
	bo.MaxInterval = 1 * time.Second
	bo.MaxElapsedTime = 5 * time.Second

	fn := func() error {
		var err error

		if err = proc.Refresh(); err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("Process with ID %d no longer exists",
					proc.Pid())

				return backoff.Permanent(err)
			}

			// Temporary failure
			return fmt.Errorf("Refreshing data on process with ID %d failed: %s",
				proc.Pid(), err)
		}

		if processMatches(proc, exe) {
			return nil
		}

		err = fmt.Errorf("Process with ID %d is not expected program %q (%+v)",
			proc.Pid(), exe, proc)

		return backoff.Permanent(err)
	}

	for {
		select {
		case <-ctx.Done():
			return false, nil
		case <-time.After(5 * time.Second):
		}

		bo.Reset()

		if err := backoff.Retry(fn, bo); err != nil {
			return true, err
		}
	}
}
