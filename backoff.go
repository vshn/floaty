package main

import (
	"time"

	"github.com/cenkalti/backoff"
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
