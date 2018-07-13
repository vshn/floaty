package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/cenkalti/backoff"
	"github.com/exoscale/egoscale"
	"github.com/exoscale/exoip"
	"github.com/sirupsen/logrus"
)

const (
	defaultExoscaleEndpoint = "https://api.exoscale.ch/compute"
)

func findExoscaleInstanceID() (string, error) {
	var instanceID string

	fn := func() error {
		mserver, err := exoip.FindMetadataServer()
		if err != nil {
			return err
		}

		logrus.Debugf("Metadata server %q", mserver)

		instanceID, err = exoip.FetchMetadata(mserver, "/latest/instance-id")

		if err == nil && len(instanceID) < 1 {
			return errors.New("Received empty instance ID")
		}

		return err
	}

	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 100 * time.Millisecond
	bo.MaxInterval = 1 * time.Second
	bo.MaxElapsedTime = 5 * time.Second
	bo.Reset()

	if err := backoff.Retry(fn, bo); err != nil {
		return "", err
	}

	return instanceID, nil
}

type exoscaleNotifyConfig struct {
	Endpoint   *textURL `yaml:"endpoint"`
	Key        string   `yaml:"key"`
	Secret     string   `yaml:"secret"`
	InstanceID string   `yaml:"instance-id"`
}

func (c exoscaleNotifyConfig) NewProvider() (elasticIPProvider, error) {
	var endpoint *url.URL
	var err error

	if c.Endpoint == nil {
		endpoint, err = url.Parse(defaultExoscaleEndpoint)
		if err != nil {
			return nil, err
		}
	} else {
		endpoint = &c.Endpoint.URL
	}

	if len(c.Key) < 1 {
		return nil, fmt.Errorf("Authentication key required")
	}

	if len(c.Secret) < 1 {
		return nil, fmt.Errorf("Authentication secret required")
	}

	instanceID := c.InstanceID

	if len(c.InstanceID) == 0 {
		if instanceID, err = findExoscaleInstanceID(); err != nil {
			return nil, fmt.Errorf("Instance ID lookup: %s", err)
		}
	}

	logrus.WithField("instance-id", instanceID).Debug("Instance ID")

	// The timeout is only used when no explicitly context is given
	client := egoscale.NewClientWithTimeout(endpoint.String(), c.Key, c.Secret, 1*time.Minute)

	vm := &egoscale.VirtualMachine{
		ID: instanceID,
	}
	if err = client.Get(vm); err != nil {
		return nil, err
	}

	nic := vm.DefaultNic()
	if nic == nil {
		return nil, fmt.Errorf("Default VM NIC not found")
	}

	return &exoscaleElasticIPProvider{
		client: client,
		nicID:  nic.ID,
	}, nil
}

type exoscaleElasticIPProvider struct {
	client *egoscale.Client
	nicID  string
}

func (p *exoscaleElasticIPProvider) NewElasticIPRefresher(logger *logrus.Entry,
	network netAddress) (elasticIPRefresher, error) {
	return &exoscaleElasticIPRefresher{
		logger:  logger,
		network: network,
		client:  p.client,
		nicID:   p.nicID,
	}, nil
}

type exoscaleElasticIPRefresher struct {
	network netAddress
	logger  *logrus.Entry
	client  *egoscale.Client
	nicID   string
}

func (r *exoscaleElasticIPRefresher) String() string {
	return r.network.String()
}

func (r *exoscaleElasticIPRefresher) Logger() *logrus.Entry {
	return r.logger
}

func (r *exoscaleElasticIPRefresher) Refresh(ctx context.Context) error {
	// Find virtual machines with IP address
	vms, err := r.client.ListWithContext(ctx, &egoscale.VirtualMachine{
		Nic: []egoscale.Nic{
			egoscale.Nic{
				IsDefault: true,
				IPAddress: r.network.IP,
			},
		},
	})
	if err != nil {
		return err
	}

	commands := []egoscale.Command{
		// Always force association to target machine
		&egoscale.AddIPToNic{
			NicID:     r.nicID,
			IPAddress: r.network.IP,
		},
	}

	// Determine which other virtual machines have the IP address associated
	// and prepare commands to disassociate
	for _, i := range vms {
		vm := i.(*egoscale.VirtualMachine)
		nic := vm.DefaultNic()

		r.logger.WithFields(logrus.Fields{
			"vm-id":   vm.ID,
			"vm-name": vm.Name,
			"vm-nic":  nic,
		}).Debugf("Virtual machine %q", vm.Name)

		if nic.ID == r.nicID {
			// Desired target
			continue
		}

		for _, secondary := range nic.SecondaryIP {
			if secondary.IPAddress.Equal(r.network.IP) {
				commands = append(commands, &egoscale.RemoveIPFromNic{
					ID: secondary.ID,
				})
			}
		}
	}

	return r.runCommands(ctx, commands)
}

// Run any number of Exoscale API commands in parallel
func (r *exoscaleElasticIPRefresher) runCommands(ctx context.Context, commands []egoscale.Command) error {
	mu := sync.Mutex{}
	wg := sync.WaitGroup{}

	var errors []error

	for _, i := range commands {
		wg.Add(1)

		go func(cmd egoscale.Command) {
			defer wg.Done()

			logger := r.logger.WithFields(logrus.Fields{
				"command":   r.client.APIName(cmd),
				"arguments": cmd,
			})

			response, err := r.client.RequestWithContext(ctx, cmd)
			if err == nil {
				logger.WithField("response", response).Info("Command successful")
				return
			}

			logger.Error(err)

			mu.Lock()
			defer mu.Unlock()

			errors = append(errors, err)
		}(i)
	}

	wg.Wait()

	if len(errors) == 0 {
		return nil
	}

	clientErrorOnly := true

	for _, err := range errors {
		if apiError, ok := err.(*egoscale.ErrorResponse); ok {
			clientErrorOnly = clientErrorOnly && (apiError.ErrorCode >= 400 && apiError.ErrorCode < 500)
		} else {
			clientErrorOnly = false
		}

		if !clientErrorOnly {
			break
		}
	}

	// Not including error details as they've been logged before
	err := fmt.Errorf("%d of %d commands failed", len(errors), len(commands))

	if clientErrorOnly {
		return backoff.Permanent(err)
	}

	return err
}
