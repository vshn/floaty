package main

import (
	"context"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type elasticIPProvider interface {
	Test(context.Context) error
	NewElasticIPRefresher(context.Context, *logrus.Entry, netAddress) (elasticIPRefresher, error)
}

type elasticIPRefresher interface {
	Logger() *logrus.Entry
	Refresh(context.Context) error
}

func pinElasticIPs(ctx context.Context, provider elasticIPProvider, addresses []netAddress, cfg notifyConfig) error {
	refreshers := []elasticIPRefresher{}
	for _, address := range addresses {
		logger := logrus.WithField("address", address)
		refresher, err := provider.NewElasticIPRefresher(ctx, logger, address)
		if err != nil {
			return err
		}
		refreshers = append(refreshers, refresher)
	}

	wg := sync.WaitGroup{}
	for _, i := range refreshers {
		wg.Add(1)
		go func(refresher elasticIPRefresher) {
			defer wg.Done()
			runRefresher(ctx, cfg.RefreshInterval, cfg.RefreshTimeout, cfg.BackOff, refresher)
		}(i)
	}
	wg.Wait()
	return nil
}

func runRefresher(ctx context.Context, interval time.Duration, timeout time.Duration, backOff backOffConfig, r elasticIPRefresher) {

	logger := r.Logger()
	logger.Infof("Refreshing %q every %s on average", r, interval)

	err := loopWithRetries(ctx, logger, interval, backOff.New(),
		func(ctx context.Context) error {
			ctxRefresh, cancel := context.WithTimeout(ctx, timeout)

			defer cancel()

			return r.Refresh(ctxRefresh)
		})

	logger.Debugf("Shutdown (%s)", err)
}
