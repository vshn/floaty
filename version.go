package main

import (
	"fmt"
	"strings"
)

// Set at build time
var commitRefName, commitSHA string

type versionInfo struct {
	VersionString string
	CommitID      string
}

func newVersionInfo() versionInfo {
	refName := commitRefName

	if len(refName) == 0 {
		refName = "ver-unknown"
	}

	id := []rune(commitSHA)

	if len(id) > 10 {
		id = id[:10]
	}

	return versionInfo{
		VersionString: refName,
		CommitID:      string(id),
	}
}

func (v versionInfo) HumanReadable() string {
	if len(v.CommitID) == 0 {
		return v.VersionString
	}

	return fmt.Sprintf("%s (commit %s)", v.VersionString, v.CommitID)
}

func (v versionInfo) HTTPUserAgent() string {
	extra := []string{
		v.VersionString,
	}

	if len(v.CommitID) > 0 {
		extra = append(extra, "commit "+v.CommitID)
	}

	return fmt.Sprintf("Ursula by APPUiO.ch (%s)", strings.Join(extra, "; "))
}
