package main

import (
	"context"

	ps "github.com/mitchellh/go-ps"
	"github.com/sirupsen/logrus"
)

const (
	keepalivedProcessName string = "keepalived"
)

type keepalivedProcess struct {
	Proc *ps.UnixProcess
}

// Attempt to find Keepalived process in process parents and wait until Keepalived has terminated and call given function once that happens
func WaitForKeepalivedTermination(ctx context.Context, stop context.CancelFunc) {
	// Keepalived does not terminate long-running notification programs when
	// exiting. In addition Keepalived may be terminated through other means
	// such as SIGKILL. In such cases the IP address updates must stop as soon
	// as possible. As of Keepalived 1.2, shipped with OpenShift 3.9, there is
	// no mechanism to reliably detect that Keepalived has terminated. Later
	// versions have support for FIFOs to communicate to notification programs.
	// Therefore the only reasonable method is to locate the process ID of
	// Keepalived and polling for its validity in a regular interval.
	keepalivedProcess, err := findKeepalivedProcessParent()
	if err != nil {
		logrus.Warningf("Keepalived not found: %s", err)
		keepalivedProcess = nil
	} else {
		go keepalivedProcess.waitForTermination(ctx, stop)
	}
}

// Attempt to find Keepalived process in process parents.
func findKeepalivedProcessParent() (*keepalivedProcess, error) {
	proc, err := findParentProcess(keepalivedProcessName)
	if err != nil {
		return nil, err
	}

	logrus.WithField("pid", proc.Pid()).Debug("Keepalived process found")

	return &keepalivedProcess{
		Proc: proc.(*ps.UnixProcess),
	}, nil
}

// Wait until Keepalived has terminated and call given function once that
// happens.
func (p *keepalivedProcess) waitForTermination(ctx context.Context,
	terminatedFunc context.CancelFunc) {

	terminated, err := waitForProcessToTerminate(ctx, p.Proc,
		keepalivedProcessName)
	if err != nil {
		logrus.Errorf("Waiting for Keepalived: %s", err)
	}

	if err != nil || terminated {
		terminatedFunc()
	}
}
