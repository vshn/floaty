package main

import (
	"io/ioutil"
	"os"
	"strconv"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/sirupsen/logrus"
)

func configOutOfMemoryKiller() error {
	// Magic "don't kill me" value as documented in
	// <https://www.kernel.org/doc/Documentation/filesystems/proc.txt>
	path := "/proc/self/oom_score_adj"
	adjustValue := strconv.Itoa(-1000)

	isPermanentError := func(err error) bool {
		return err != nil && os.IsPermission(err)
	}

	fn := func() error {
		err := ioutil.WriteFile(path, []byte(adjustValue), 0600)

		if isPermanentError(err) {
			return backoff.Permanent(err)
		}

		return err
	}

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 10 * time.Millisecond
	bo.MaxInterval = 100 * time.Second
	bo.MaxElapsedTime = 1 * time.Second
	bo.Reset()

	err := backoff.Retry(fn, bo)

	if isPermanentError(err) {
		logrus.Warningf("Setting OOM adjust score in %q: %s", path, err)
		return nil
	}

	return err
}
