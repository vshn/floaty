package main

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/nightlyone/lockfile"
	"github.com/sirupsen/logrus"
)

// Attempt to acquire a file-based lock or, if that isn't possible within
// a configurable amount of time it will return an error. If there is
// already a process owning the lock it's sent a SIGTERM signal.
func acquireLock(ctx context.Context, path string, timeout time.Duration) (func() error, error) {
	sentSIGTERM := false

	lock, err := lockfile.New(path)
	if err != nil {
		return nil, err
	}

	fn := func() error {
		err := lock.TryLock()

		if err == nil {
			return nil
		}

		if err == lockfile.ErrBusy && !sentSIGTERM {
			if proc, errOwner := lock.GetOwner(); errOwner == nil && proc != nil {
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
	bo.MaxElapsedTime = timeout
	bo.Reset()

	if err := backoff.Retry(fn, bo); err != nil {
		logrus.Fatal(err)
	}

	if proc, err := lock.GetOwner(); err != nil {
		return nil, fmt.Errorf("Getting lock owner: %s", err)
	} else if proc.Pid != os.Getpid() {
		return nil, fmt.Errorf("Lock owned by PID %d", proc.Pid)
	}

	logrus.Debugf("Lock on file %q acquired", lock)
	return lock.Unlock, nil
}
