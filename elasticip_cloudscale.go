package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/sirupsen/logrus"

	cloudscale "github.com/cloudscale-ch/cloudscale-go-sdk"
	"github.com/google/uuid"
)

type cloudscaleNotifyConfig struct {
	Endpoint *textURL `yaml:"endpoint"`
	Token    string   `yaml:"token"`

	ServerUUID           uuid.UUID            `yaml:"server-uuid"`
	HostnameToServerUUID map[string]uuid.UUID `yaml:"hostname-to-server-uuid"`
}

func (cfg cloudscaleNotifyConfig) findServerUUID(hostname string) (uuid.UUID, error) {
	nullUUID := uuid.UUID{}

	if cfg.ServerUUID != nullUUID {
		// Directly specified in config
		return cfg.ServerUUID, nil
	}

	if serverUUID, ok := cfg.HostnameToServerUUID[hostname]; ok && serverUUID != nullUUID {
		// Found using hostname
		return serverUUID, nil
	}

	return nullUUID, fmt.Errorf("Server UUID not found with hostname %q", hostname)
}

func (cfg cloudscaleNotifyConfig) NewProvider() (elasticIPProvider, error) {
	if len(cfg.Token) < 1 {
		return nil, fmt.Errorf("Authentication token required")
	}

	httpClient := &http.Client{
		Timeout: 1 * time.Minute,
	}

	client := cloudscale.NewClient(httpClient)
	client.UserAgent = newVersionInfo().HTTPUserAgent()
	client.AuthToken = cfg.Token

	if cfg.Endpoint != nil {
		// Make copy to prevent modifications
		baseURL := url.URL(cfg.Endpoint.URL)

		client.BaseURL = &baseURL
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("Retrieving hostname: %s", err)
	}

	logrus.Debugf("Hostname %q", hostname)

	serverUUID, err := cfg.findServerUUID(hostname)
	if err != nil {
		return nil, err
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

func (p *cloudscaleFloatingIPProvider) Test(ctx context.Context) error {
	var errServer, errFloatingIP error
	var server *cloudscale.Server
	var floatingIPs []cloudscale.FloatingIP

	wg := sync.WaitGroup{}
	wg.Add(2)

	go func() {
		defer wg.Done()
		server, errServer = p.client.Servers.Get(ctx, p.serverUUID)
	}()

	go func() {
		defer wg.Done()
		floatingIPs, errFloatingIP = p.client.FloatingIPs.List(ctx)
	}()

	wg.Wait()

	fields := logrus.Fields{}
	success := true

	if errServer == nil {
		fields["server"] = server
	} else {
		success = false
		logrus.Errorf("Retrieving server %q: %s", p.serverUUID, errServer)
	}

	if errFloatingIP == nil {
		fields["floating-ips"] = floatingIPs
	} else {
		success = false
		logrus.Errorf("Listing floating IPs failed: %s", errFloatingIP)
	}

	logger := logrus.WithFields(fields)

	if success {
		logger.Debug("Test successful")
		return nil
	}

	logger.Error("Test failed")

	return errors.New("Self-test failed")
}

func (p *cloudscaleFloatingIPProvider) NewElasticIPRefresher(logger *logrus.Entry,
	network netAddress) (elasticIPRefresher, error) {
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

	r.logger.Infof("Set next-hop of address %s to server %s", ip, serverUUID)

	req := &cloudscale.FloatingIPUpdateRequest{
		Server: serverUUID,
	}

	response, err := client.FloatingIPs.Update(ctx, ip, req)

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
