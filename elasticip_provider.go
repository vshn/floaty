package main

import (
	"context"

	"github.com/sirupsen/logrus"
)

type elasticIPProvider interface {
	NewElasticIPRefresher(notifyConfig, netAddress) (elasticIPRefresher, error)
}

type elasticIPRefresher interface {
	Logger() *logrus.Entry
	Refresh(context.Context) error
}

func runRefresher(ctx context.Context, cfg notifyConfig, r elasticIPRefresher) {
	interval := cfg.RefreshInterval

	logger := r.Logger()
	logger.Infof("Refreshing %q every %s on average", r, interval)

	err := loopWithRetries(ctx, logger, interval, cfg.BackOff.New(), r.Refresh)

	logger.Debugf("Shutdown (%s)", err)
}
