package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"time"

	yaml "gopkg.in/yaml.v2"
)

const (
	defaultLockFileTemplate = "/var/lock/floaty.%s.lock"
	defaultLockTimeout      = 10 * time.Second

	defaultKeepalivedConfigFile = "/etc/keepalived/keepalived.conf"

	defaultRefreshInterval = 1 * time.Minute
	defaultRefreshTimeout  = 10 * time.Second
)

type notifyConfig struct {
	LockFileTemplate string        `yaml:"lock-file-template"`
	LockTimeout      time.Duration `yaml:"lock-timeout"`

	KeepalivedConfigFile string `yaml:"keepalived-config"`

	ManagedAddresses []netAddress `yaml:"managed-addresses"`

	RefreshInterval time.Duration `yaml:"refresh-interval"`
	RefreshTimeout  time.Duration `yaml:"refresh-timeout"`

	BackOff backOffConfig `yaml:"back-off"`

	Provider   string                 `yaml:"provider"`
	Cloudscale cloudscaleNotifyConfig `yaml:"cloudscale"`
	Exoscale   exoscaleNotifyConfig   `yaml:"exoscale"`
}

func newNotifyConfig() notifyConfig {
	return notifyConfig{
		LockFileTemplate:     defaultLockFileTemplate,
		LockTimeout:          defaultLockTimeout,
		KeepalivedConfigFile: defaultKeepalivedConfigFile,
		RefreshInterval:      defaultRefreshInterval,
		RefreshTimeout:       defaultRefreshTimeout,
		BackOff:              newBackOffConfig(),
	}
}

// Update configuration from a YAML file
func (c *notifyConfig) ReadFromYAML(path string) error {
	content, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}

	return yaml.UnmarshalStrict(content, c)
}

func (c notifyConfig) NewProvider() (elasticIPProvider, error) {
	switch c.Provider {
	case "":
		return nil, errors.New("Missing provider")

	case "cloudscale":
		return c.Cloudscale.NewProvider()

	case "exoscale":
		return c.Exoscale.NewProvider()

	case "fake":
		return NewFakeProvider()
	}

	return nil, fmt.Errorf("Provider %q not supported", c.Provider)
}

func (c notifyConfig) MakeLockFilePath(name string) string {
	return fmt.Sprintf(c.LockFileTemplate, url.PathEscape(name))
}

func (c notifyConfig) getAddresses(vrrpInstanceName string) ([]netAddress, error) {
	if len(c.ManagedAddresses) > 0 {
		return c.ManagedAddresses, nil
	}
	return readAddressesFromKeepalivedConfig(c.KeepalivedConfigFile, vrrpInstanceName)
}

func readAddressesFromKeepalivedConfig(path, vrrpInstanceName string) ([]netAddress, error) {
	parsed, err := parseKeepalivedConfigFile(path)
	if err != nil {
		return nil, err
	}

	vrrpInstance, ok := parsed.vrrpInstances[vrrpInstanceName]
	if !ok {
		return nil, fmt.Errorf("No VRRP instance named %q", vrrpInstanceName)
	}

	return vrrpInstance.Addresses, nil
}

func loadConfig(path string, dryRun bool) (notifyConfig, error) {
	cfg := newNotifyConfig()

	if err := cfg.ReadFromYAML(path); err != nil {
		return cfg, err
	}

	return cfg, nil
}
