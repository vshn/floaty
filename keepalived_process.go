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
