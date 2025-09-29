package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
)

func NewFakeProvider() (elasticIPProvider, error) {
	return &fakeElasticIPProvider{}, nil
}

type fakeElasticIPProvider struct {
	mu             sync.Mutex
	refreshCounter map[string]int
}

func (p *fakeElasticIPProvider) Test(ctx context.Context) error {
	return nil
}

func (p *fakeElasticIPProvider) NewElasticIPRefresher(ctx context.Context,
	logger *logrus.Entry, network netAddress) (elasticIPRefresher, error) {

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.refreshCounter == nil {
		p.refreshCounter = map[string]int{}
	}
	ref := &fakeElasticIPRefresher{
		network:        network,
		logger:         logger,
		mu:             &p.mu,
		refreshCounter: p.refreshCounter,
	}

	return ref, nil
}

type fakeElasticIPRefresher struct {
	network netAddress
	logger  *logrus.Entry

	mu             *sync.Mutex
	refreshCounter map[string]int
}

func (r *fakeElasticIPRefresher) Logger() *logrus.Entry {
	return r.logger
}

func (r *fakeElasticIPRefresher) Refresh(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	c := r.refreshCounter[r.network.String()]
	r.refreshCounter[r.network.String()] = c + 1

	fmt.Printf("REFRESH %s\n", r.network)
	return nil
}
