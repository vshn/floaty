package main

import (
	"context"

	"github.com/sirupsen/logrus"
)

type elasticIPProvider interface {
	Test(context.Context) error
	NewElasticIPRefresher(*logrus.Entry, netAddress) (elasticIPRefresher, error)
}

type elasticIPRefresher interface {
	Logger() *logrus.Entry
	Refresh(context.Context) error
}

func runRefresher(ctx context.Context, cfg notifyConfig, r elasticIPRefresher) {
	interval := cfg.RefreshInterval

	logger := r.Logger()
	logger.Infof("Refreshing %q every %s on average", r, interval)

	err := loopWithRetries(ctx, logger, interval, cfg.BackOff.New(),
		func(ctx context.Context) error {
			ctxRefresh, cancel := context.WithTimeout(ctx, cfg.RefreshTimeout)

			defer cancel()

			return r.Refresh(ctxRefresh)
		})

	logger.Debugf("Shutdown (%s)", err)
}
