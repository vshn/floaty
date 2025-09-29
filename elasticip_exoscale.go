package main

import (
	"context"
	"fmt"
	"net"
	"time"

	egoscale "github.com/exoscale/egoscale/v3"
	"github.com/exoscale/egoscale/v3/credentials"
	"github.com/exoscale/exoip"
	"github.com/sirupsen/logrus"
	"go.uber.org/multierr"
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

func findExoscaleInstanceID() (egoscale.UUID, error) {
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

	return egoscale.ParseUUID(instanceID)
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

	var instanceID egoscale.UUID
	if c.InstanceID == "" {
		if instanceID, err = findExoscaleInstanceID(); err != nil {
			return nil, fmt.Errorf("Instance ID lookup: %s", err)
		}
	} else {
		if instanceID, err = egoscale.ParseUUID(c.InstanceID); err != nil {
			return nil, fmt.Errorf("Failed to parse instance UUID: %s", err)
		}
	}

	logrus.WithField("instance-id", instanceID.String()).Debug("Instance ID")

	creds := credentials.NewStaticCredentials(c.Key, c.Secret)

	timeoutOpt := egoscale.ClientOptWithWaitTimeout(1 * time.Minute)
	client, err := egoscale.NewClient(creds, timeoutOpt)
	if err != nil {
		return nil, err
	}

	// NOTE(sg): egoscale/v3 provides no sane way to get the zone API URL
	// from the zone name without listing all zones on the API, so we
	// build the URL ourselves here.
	zoneEndpoint := egoscale.Endpoint(fmt.Sprintf("https://api-%s.exoscale.com/v2", zone))
	if c.Endpoint != nil {
		logrus.WithField("endpoint", c.Endpoint.String()).Info("Using custom API endpoint")
		zoneEndpoint = egoscale.Endpoint(c.Endpoint.String())
	}
	client = client.WithEndpoint(zoneEndpoint)

	vm, err := client.GetInstance(context.Background(), instanceID)
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
	eips, err := p.client.ListElasticIPS(ctx)
	if err != nil {
		return err
	}

	elasticIPs := []string{}
	for _, eip := range eips.ElasticIPS {
		elasticIPs = append(elasticIPs, eip.IP)
	}
	logrus.WithField("eips", elasticIPs).Debug("Got elastic IPs")

	vms, err := p.client.ListInstances(ctx)
	if err != nil {
		return err
	}

	instances := []string{}
	for _, vm := range vms.Instances {
		instances = append(instances, vm.ID.String())
	}
	logrus.WithField("count", len(instances)).WithField("instances", instances).Debug("Got instances")

	return nil
}

func (p *exoscaleElasticIPProvider) NewElasticIPRefresher(logger *logrus.Entry,
	network netAddress) (elasticIPRefresher, error) {
	eips, err := p.client.ListElasticIPS(context.Background())
	if err != nil {
		return nil, fmt.Errorf("Elastic IP lookup: %s", err)
	}
	for _, eip := range eips.ElasticIPS {
		ip := net.ParseIP(eip.IP)
		if ip == nil {
			logrus.WithField("eip", eip.IP).Warn("Failed to parse EIP")
			continue
		}
		logrus.WithField("eip", ip).Debug("Checking EIP")
		if network.Contains(ip) {
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
	eip      egoscale.ElasticIP
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
	target := egoscale.AttachInstanceToElasticIPRequest{
		Instance: &egoscale.InstanceTarget{
			ID: r.instance.ID,
		},
	}
	op, err := r.client.AttachInstanceToElasticIP(ctx, r.eip.ID, target)
	if err != nil {
		return fmt.Errorf("while attaching the IP to this instance: %s", err)
	}
	op, err = r.client.Wait(ctx, op, egoscale.OperationStateSuccess)
	if err != nil {
		return fmt.Errorf("while attaching the IP to this instance: %s", err)
	}
	logrus.Infof("Ensured that %s is attached to instance %s", r.eip.IP, r.instance.ID.String())

	vms, err := r.client.ListInstances(ctx)
	if err != nil {
		return fmt.Errorf("Unable to list instances: %s", err)
	}

	// Detach from other instances
	var detacherrs error
	for _, vm := range vms.Instances {
		if vm.ID == r.instance.ID {
			continue
		}
		// NOTE(sg): the response from `ListInstances()` doesn't
		// contain the attached EIPs. Because of that we need to fetch
		// the instance details with `GetInstance()` in order to be
		// able to detach EIPs from other instances.
		vmdetails, err := r.client.GetInstance(ctx, vm.ID)
		if err != nil {
			detacherrs = multierr.Append(detacherrs, err)
			continue
		}
		if vmdetails.ElasticIPS != nil && len(vmdetails.ElasticIPS) > 0 {
			logrus.Debugf("Checking if we need to detach EIPs from %s", vm.ID.String())
			for _, eip := range vmdetails.ElasticIPS {
				if eip.ID == r.eip.ID {
					logrus.Infof("Detaching EIP %s from %s", r.eip.IP, vm.ID.String())
					detachTarget := egoscale.DetachInstanceFromElasticIPRequest{
						Instance: &egoscale.InstanceTarget{
							ID: vm.ID,
						},
					}
					op, err := r.client.DetachInstanceFromElasticIP(ctx, r.eip.ID, detachTarget)
					if err != nil {
						detacherrs = multierr.Append(detacherrs, err)
					}
					op, err = r.client.Wait(ctx, op, egoscale.OperationStateSuccess)
					if err != nil {
						detacherrs = multierr.Append(detacherrs, err)
					}
				}
			}
		}
	}
	return detacherrs
}
