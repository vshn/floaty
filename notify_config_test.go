package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadConfig(t *testing.T) {
	cfg, err := loadConfig("floaty.example.yaml", false)
	assert.NoError(t, err)
	assert.Equalf(t, "cloudscale", cfg.Provider, "error parsing provider from config file")
	assert.Equalf(t, "fake-token", cfg.Cloudscale.Token, "error parsing cloudscale token from config file")
	managedAddr := netAddress{}
	managedAddr.UnmarshalText([]byte("192.0.2.10"))
	assert.Equalf(t, []netAddress{managedAddr}, cfg.ManagedAddresses, "error parsing managed addresses from config file")
}
