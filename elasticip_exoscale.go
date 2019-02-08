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
	uuid "github.com/satori/go.uuid"
	"github.com/sirupsen/logrus"
)

const (
	defaultExoscaleEndpoint = "https://api.exoscale.ch/compute"
)

func findExoscaleInstanceID() (uuid.UUID, error) {
	var instanceID uuid.UUID

	fn := func() error {
		mserver, err := exoip.FindMetadataServer()
		if err != nil {
			return err
		}

		logrus.Debugf("Metadata server %q", mserver)

		rawInstanceID, err := exoip.FetchMetadata(mserver, "/latest/instance-id")

		if err == nil && len(rawInstanceID) < 1 {
			return errors.New("Received empty instance ID")
		}

		instanceID, err = uuid.FromString(rawInstanceID)

		return err
	}

	if err := metadataRetry(fn); err != nil {
		return uuid.Nil, err
	}

	return instanceID, nil
}

type exoscaleNotifyConfig struct {
	Endpoint   *textURL  `yaml:"endpoint"`
	Key        string    `yaml:"key"`
	Secret     string    `yaml:"secret"`
	InstanceID uuid.UUID `yaml:"instance-id"`
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

	if uuid.Equal(c.InstanceID, uuid.Nil) {
		if instanceID, err = findExoscaleInstanceID(); err != nil {
			return nil, fmt.Errorf("Instance ID lookup: %s", err)
		}
	}

	logrus.WithField("instance-id", instanceID).Debug("Instance ID")

	client := egoscale.NewClient(endpoint.String(), c.Key, c.Secret)

	// The timeout is only used when no context is given to API invocations
	client.Timeout = 1 * time.Minute

	resp, err := client.Get(
		egoscale.VirtualMachine{
			ID: &egoscale.UUID{
				UUID: instanceID,
			},
		})
	if err != nil {
		return nil, err
	}

	vm := resp.(*egoscale.VirtualMachine)

	nic := vm.DefaultNic()
	if nic == nil {
		return nil, fmt.Errorf("Default VM NIC not found")
	}

	return &exoscaleElasticIPProvider{
		client:     client,
		instanceID: vm.ID,
		nicID:      nic.ID,
	}, nil
}

type exoscaleElasticIPProvider struct {
	client     *egoscale.Client
	instanceID *egoscale.UUID
	nicID      *egoscale.UUID
}

func (p *exoscaleElasticIPProvider) Test(ctx context.Context) error {
	nic, err := p.client.ListWithContext(ctx, &egoscale.Nic{
		VirtualMachineID: p.instanceID,
		ID:               p.nicID,
	})
	if err != nil {
		return fmt.Errorf("Retrieving NIC %q: %s", p.nicID, err)
	}

	logrus.WithField("nic", nic).Info("Test successful")

	return nil
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
	nicID   *egoscale.UUID
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

		if nic.ID != nil && r.nicID.Equal(*nic.ID) {
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
