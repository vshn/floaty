package main

import (
	"time"

	"github.com/cenkalti/backoff"
	"github.com/sirupsen/logrus"
)

type backOffConfig struct {
	InitialInterval time.Duration `yaml:"initial-interval"`
	Multiplier      float64       `yaml:"multiplier"`
	MaxInterval     time.Duration `yaml:"max-interval"`
	MaxElapsedTime  time.Duration `yaml:"max-elapsed-time"`
}

func newBackOffConfig() backOffConfig {
	return backOffConfig{
		InitialInterval: 1 * time.Second,
		Multiplier:      1.1,
		MaxInterval:     10 * time.Second,
		MaxElapsedTime:  0,
	}
}

func (cfg *backOffConfig) New() backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = cfg.InitialInterval
	b.Multiplier = cfg.Multiplier
	b.MaxInterval = cfg.MaxInterval
	b.MaxElapsedTime = cfg.MaxElapsedTime
	b.Reset()
	return b
}

// metadataRetry is a utility function retry metadata-receiving functions
func metadataRetry(operation backoff.Operation) error {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 100 * time.Millisecond
	bo.MaxInterval = 1 * time.Second
	bo.MaxElapsedTime = 10 * time.Second
	bo.Reset()

	return backoff.Retry(func() error {
		if err := operation(); err != nil {
			logrus.Debug(err)
			return err
		}

		return nil
	}, bo)
}
