package main

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
)

func NewFakeProvider() (elasticIPProvider, error) {
	return &fakeElasticIPProvider{}, nil
}

type fakeElasticIPProvider struct {
}

func (p *fakeElasticIPProvider) Test(ctx context.Context) error {
	return nil
}

func (p *fakeElasticIPProvider) NewElasticIPRefresher(logger *logrus.Entry,
	network netAddress) (elasticIPRefresher, error) {

	ref := &fakeElasticIPRefresher{
		logger:  logger,
		network: network,
	}

	return ref, nil
}

type fakeElasticIPRefresher struct {
	network netAddress
	logger  *logrus.Entry
}

func (r *fakeElasticIPRefresher) Logger() *logrus.Entry {
	return r.logger
}

func (r *fakeElasticIPRefresher) Refresh(ctx context.Context) error {
	fmt.Printf("REFRESH %s\n", r.network)
	return nil
}
