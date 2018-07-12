package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/sirupsen/logrus"

	cloudscale "github.com/cloudscale-ch/cloudscale-go-sdk"
	"github.com/google/uuid"
)

type cloudscaleNotifyConfig struct {
	Endpoint string `yaml:"endpoint"`
	Token    string `yaml:"token"`

	ServerUUID           uuid.UUID            `yaml:"server-uuid"`
	HostnameToServerUUID map[string]uuid.UUID `yaml:"hostname-to-server-uuid"`
}

func (cfg cloudscaleNotifyConfig) NewProvider(globalConfig notifyConfig) (elasticIPProvider, error) {
	if len(cfg.Token) < 1 {
		return nil, fmt.Errorf("Authentication token required")
	}

	httpClient := &http.Client{
		Timeout: globalConfig.RefreshTimeout,
	}

	client := cloudscale.NewClient(httpClient)
	client.UserAgent = newVersionInfo().HTTPUserAgent()
	client.AuthToken = cfg.Token

	if len(cfg.Endpoint) > 0 {
		var err error

		if client.BaseURL, err = url.Parse(cfg.Endpoint); err != nil {
			return nil, err
		}
	}

	nullUUID := uuid.UUID{}
	serverUUID := cfg.ServerUUID

	if serverUUID == nullUUID {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, fmt.Errorf("Retrieving hostname: %s", err)
		}

		var ok bool

		serverUUID, ok = cfg.HostnameToServerUUID[hostname]
		if !ok {
			return nil, fmt.Errorf("Server UUID not found for hostname %q", hostname)
		}
	}

	if serverUUID == nullUUID {
		return nil, errors.New("Server UUID is required")
	}

	switch serverUUID.Variant() {
	case uuid.RFC4122, uuid.Microsoft:
		break
	default:
		return nil, fmt.Errorf("Invalid UUID %q", serverUUID)
	}

	return &cloudscaleFloatingIPProvider{
		serverUUID: serverUUID.String(),
		httpClient: httpClient,
		client:     client,
	}, nil
}

type cloudscaleFloatingIPProvider struct {
	serverUUID string
	httpClient *http.Client
	client     *cloudscale.Client
}

func (p *cloudscaleFloatingIPProvider) NewElasticIPRefresher(_ notifyConfig,
	network netAddress) (elasticIPRefresher, error) {

	logger := logrus.WithField("address", network.String())

	return &cloudscaleFloatingIPRefresher{
		provider: p,
		network:  network,
		logger:   logger,
	}, nil
}

type cloudscaleFloatingIPRefresher struct {
	provider *cloudscaleFloatingIPProvider
	client   *cloudscale.Client
	network  netAddress
	logger   *logrus.Entry
}

func (r *cloudscaleFloatingIPRefresher) String() string {
	return r.network.String()
}

func (r *cloudscaleFloatingIPRefresher) Logger() *logrus.Entry {
	return r.logger
}

func (r *cloudscaleFloatingIPRefresher) Refresh(ctx context.Context) error {
	serverUUID := r.provider.serverUUID
	ip := r.network.IP.String()

	client := r.provider.client
	httpClient := r.provider.httpClient
	timeout := httpClient.Timeout

	r.logger.Infof("Set next-hop of address %s to server %s", ip, serverUUID)

	extraTime :=
		time.Duration(math.Min(10, math.Max(2, timeout.Seconds()/10.0))) * time.Second

	// Give slightly more time than for underlying HTTP timeout
	ctxRequest, ctxCancel := context.WithTimeout(ctx, timeout+extraTime)

	defer ctxCancel()

	req := &cloudscale.FloatingIPUpdateRequest{
		Server: serverUUID,
	}

	response, err := client.FloatingIPs.Update(ctxRequest, ip, req)

	if err == nil {
		r.logger.WithField("response", response).Debug("Refresh successful")
		return nil
	}

	r.logger.Errorf("Setting next-hop of address %s to server %s failed: %s",
		ip, serverUUID, err)

	if apiError, ok := err.(*cloudscale.ErrorResponse); ok {
		if apiError.StatusCode >= 400 && apiError.StatusCode < 500 {
			// Client error
			return backoff.Permanent(apiError)
		}
	}

	return err
}
