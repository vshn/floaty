package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmpty(t *testing.T) {
	reader := strings.NewReader("")
	cfg, err := parseKeepalivedConfig(reader)
	if assert.NoError(t, err) {
		assert.Equal(t, cfg, &keepalivedConfig{
			vrrpInstances: make(map[string]*keepalivedConfigVrrpInstance),
		})
	}
}

func TestSimple(t *testing.T) {
	reader := strings.NewReader(`
	vrrp_instance foo {
		virtual_ipaddress {
			192.0.2.200/32
			192.0.2.201/24
		}
	}
	vrrp_instance empty {
	}
	vrrp_instance ipfailover_VIP_2 {
		interface eth1
		state MASTER
		virtual_router_id 2
		priority 142
		preempt_delay 300

		authentication {
			auth_type PASS
			auth_pass ipfailover
		}

		track_script {
			chk_ipfailover
		}

		notify "/utils/notify"
		virtual_ipaddress {
			192.0.2.101 dev eth1
			192.0.2.102
		}
	}
	vrrp_instance last {
		virtual_ipaddress {
			2001:db8::ff00
			2001:db8::/64
			192.0.2.100/1
		}
	}
	`)

	cfg, err := parseKeepalivedConfig(reader)
	if assert.NoError(t, err) {
		expected := map[string]*keepalivedConfigVrrpInstance{
			"foo": &keepalivedConfigVrrpInstance{
				Name: "foo",
				Addresses: []netAddress{
					mustParseNetAddress("192.0.2.200/32"),
					mustParseNetAddress("192.0.2.201/24"),
				},
			},
			"empty": &keepalivedConfigVrrpInstance{
				Name: "empty",
			},
			"ipfailover_VIP_2": &keepalivedConfigVrrpInstance{
				Name: "ipfailover_VIP_2",
				Addresses: []netAddress{
					mustParseNetAddress("192.0.2.101/32"),
					mustParseNetAddress("192.0.2.102"),
				},
			},
			"last": &keepalivedConfigVrrpInstance{
				Name: "last",
				Addresses: []netAddress{
					mustParseNetAddress("2001:db8::ff00"),
					mustParseNetAddress("2001:db8::/64"),
					mustParseNetAddress("192.0.2.100/1"),
				},
			},
		}

		assert.Equal(t, cfg.vrrpInstances, expected)
	}
}

func TestFaultyIPAddress(t *testing.T) {
	reader := strings.NewReader(`
	vrrp_instance bar {
	}
	vrrp_instance foo {
		virtual_ipaddress {
			0.invalid.ip.address
		}
	}
	`)

	cfg, err := parseKeepalivedConfig(reader)
	assert.Nil(t, cfg)
	if assert.Error(t, err) {
		assert.Equal(t, err.Error(),
			`Line 6: Parsing IP address "0.invalid.ip.address" failed`)
	}
}

/*
func TestUnterminatedVRRPInstance(t *testing.T) {
	reader := strings.NewReader(`
	vrrp_instance bar {
		virtual_ipaddress {
			192.0.2.100
		}
	}
	vrrp_instance foo {
	`)

	cfg, err := parseKeepalivedConfig(reader)
	assert.Nil(t, cfg)
	if assert.Error(t, err) {
		assert.Equal(t, err.Error(),
			`End-of-file: Unterminated VRRP instance "foo"`)
	}
}
*/

func TestDuplicatedVRRPInstanceName(t *testing.T) {
	reader := strings.NewReader(`
	vrrp_instance hello {
	}
	vrrp_instance bar {
	}
	vrrp_instance hello {
	}
	`)

	cfg, err := parseKeepalivedConfig(reader)
	assert.Nil(t, cfg)
	if assert.Error(t, err) {
		assert.Equal(t, err.Error(),
			`Line 6: Duplicate VRRP instance name "hello"`)
	}
}
