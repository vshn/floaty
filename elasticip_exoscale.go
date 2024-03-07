package main

import (
	"context"
	"fmt"
	"time"

	egoscale "github.com/exoscale/egoscale/v2"
	"github.com/exoscale/exoip"
	"github.com/sirupsen/logrus"
	"go.uber.org/multierr"
)

const (
	defaultExoscaleEndpoint = "https://api.exoscale.ch/compute"
)

func findExoscaleZone() (string, error) {
	var zone string

	fn := func() error {
		mserver, err := exoip.FindMetadataServer()
		if err != nil {
			return err
		}

		logrus.Debugf("Metadata server %q", mserver)

		zone, err = exoip.FetchMetadata(mserver, "/latest/availability-zone")

		return err
	}

	if err := metadataRetry(fn); err != nil {
		return "", err
	}

	return zone, nil
}

func findExoscaleInstanceID() (string, error) {
	var instanceID string

	fn := func() error {
		mserver, err := exoip.FindMetadataServer()
		if err != nil {
			return err
		}

		logrus.Debugf("Metadata server %q", mserver)

		instanceID, err = exoip.FetchMetadata(mserver, "/latest/instance-id")

		return err
	}

	if err := metadataRetry(fn); err != nil {
		return "", err
	}

	return instanceID, nil
}

type exoscaleNotifyConfig struct {
	Endpoint   *textURL `yaml:"endpoint"`
	Zone       string   `yaml:"zone"`
	Key        string   `yaml:"key"`
	Secret     string   `yaml:"secret"`
	InstanceID string   `yaml:"instance-id"`
}

func (c exoscaleNotifyConfig) NewProvider() (elasticIPProvider, error) {
	var err error

	if len(c.Key) < 1 {
		return nil, fmt.Errorf("Authentication key required")
	}

	if len(c.Secret) < 1 {
		return nil, fmt.Errorf("Authentication secret required")
	}

	zone := c.Zone
	if zone == "" {
		if zone, err = findExoscaleZone(); err != nil {
			return nil, fmt.Errorf("Exoscale zone lookup: %s", err)
		}
	}
	logrus.WithField("zone", zone).Debug("Exoscale zone")

	instanceID := c.InstanceID

	if c.InstanceID == "" {
		if instanceID, err = findExoscaleInstanceID(); err != nil {
			return nil, fmt.Errorf("Instance ID lookup: %s", err)
		}
	}

	logrus.WithField("instance-id", instanceID).Debug("Instance ID")

	timeoutOpt := egoscale.ClientOptWithTimeout(1 * time.Minute)
	client, err := egoscale.NewClient(c.Key, c.Secret, timeoutOpt)
	if err != nil {
		return nil, err
	}

	vm, err := client.GetInstance(context.Background(), zone, instanceID)
	if err != nil {
		return nil, err
	}

	return &exoscaleElasticIPProvider{
		client:   client,
		zone:     zone,
		instance: vm,
	}, nil
}

type exoscaleElasticIPProvider struct {
	client   *egoscale.Client
	zone     string
	instance *egoscale.Instance
}

func (p *exoscaleElasticIPProvider) Test(ctx context.Context) error {
	// Check that we can list EIPs and instances
	eips, err := p.client.ListElasticIPs(ctx, p.zone)
	if err != nil {
		return err
	}

	elasticIPs := []string{}
	for _, eip := range eips {
		elasticIPs = append(elasticIPs, eip.IPAddress.String())
	}
	logrus.WithField("eips", elasticIPs).Debug("Got elastic IPs")

	vms, err := p.client.ListInstances(ctx, p.zone)
	if err != nil {
		return err
	}

	instances := []string{}
	for _, vm := range vms {
		instances = append(instances, *vm.ID)
	}
	logrus.WithField("instances", instances).Debug("Got instances")

	return nil
}

func (p *exoscaleElasticIPProvider) NewElasticIPRefresher(logger *logrus.Entry,
	network netAddress) (elasticIPRefresher, error) {
	eips, err := p.client.ListElasticIPs(context.Background(), p.zone)
	if err != nil {
		return nil, fmt.Errorf("Elastic IP lookup: %s", err)
	}
	for _, eip := range eips {
		logrus.WithField("eip", eip.IPAddress).Debug("Checking EIP")
		if network.Contains(*eip.IPAddress) {
			return &exoscaleElasticIPRefresher{
				logger:   logger,
				network:  network,
				client:   p.client,
				eip:      eip,
				instance: p.instance,
				zone:     p.zone,
			}, nil
		}
	}
	return nil, fmt.Errorf("Unable to find elastic IP for %s", network)
}

type exoscaleElasticIPRefresher struct {
	network  netAddress
	logger   *logrus.Entry
	client   *egoscale.Client
	eip      *egoscale.ElasticIP
	instance *egoscale.Instance
	zone     string
}

func (r *exoscaleElasticIPRefresher) String() string {
	return r.network.String()
}

func (r *exoscaleElasticIPRefresher) Logger() *logrus.Entry {
	return r.logger
}

func (r *exoscaleElasticIPRefresher) Refresh(ctx context.Context) error {
	err := r.client.AttachInstanceToElasticIP(ctx, r.zone, r.instance, r.eip)
	if err != nil {
		return fmt.Errorf("while attaching the IP to this instance: %s", err)
	}
	logrus.Infof("Ensured that %s is attached to instance %s", r.eip.IPAddress, *r.instance.ID)

	vms, err := r.client.ListInstances(ctx, r.zone)
	if err != nil {
		return fmt.Errorf("Unable to list instances: %s", err)
	}

	// Detach from other instances
	var detacherrs error
	for _, vm := range vms {
		if *vm.ID == *r.instance.ID {
			continue
		}
		// NOTE(sg): the response from `ListInstances()` doesn't
		// contain the attached EIPs. Because of that we need to fetch
		// the instance details with `GetInstance()` in order to be
		// able to detach EIPs from other instances.
		vmdetails, err := r.client.GetInstance(ctx, r.zone, *vm.ID)
		if err != nil {
			detacherrs = multierr.Append(detacherrs, err)
			continue
		}
		if vmdetails.ElasticIPIDs != nil {
			logrus.Debugf("Checking if we need to detach EIPs from %s", *vm.ID)
			for _, eip := range *vmdetails.ElasticIPIDs {
				if eip == *r.eip.ID {
					logrus.Infof("Detaching EIP %s from %s", r.eip.IPAddress, *vm.ID)
					err := r.client.DetachInstanceFromElasticIP(ctx, r.zone, vm, r.eip)
					if err != nil {
						detacherrs = multierr.Append(detacherrs, err)
					}
				}
			}
		}
	}
	return detacherrs
}
