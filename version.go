package main

import (
	"fmt"
	"strings"
)

// Set at build time
var version, commit, date string

type versionInfo struct {
	VersionString string
	CommitID      string
	Date          string
}

func newVersionInfo() versionInfo {
	refName := version

	if len(refName) == 0 {
		refName = "ver-unknown"
	}

	id := []rune(commit)

	if len(id) > 10 {
		id = id[:10]
	}

	return versionInfo{
		VersionString: refName,
		CommitID:      string(id),
		Date:          date,
	}
}

func (v versionInfo) HumanReadable() string {
	if len(v.CommitID) == 0 {
		return v.VersionString
	}

	return fmt.Sprintf("%s (commit %s, %s)", v.VersionString, v.CommitID, v.Date)
}

func (v versionInfo) HTTPUserAgent() string {
	extra := []string{
		v.VersionString,
	}

	if len(v.CommitID) > 0 {
		extra = append(extra, "commit "+v.CommitID)
		extra = append(extra, "date "+v.Date)
	}

	return fmt.Sprintf("Floaty by vshn.ch (%s)", strings.Join(extra, "; "))
}
