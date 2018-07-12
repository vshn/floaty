package main

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseIPv4(t *testing.T) {
	addr, err := parseNetAddress("192.0.2.73")
	assert.NoError(t, err)
	assert.True(t, addr.IP.Equal(net.IPv4(192, 0, 2, 73)))
	assert.Equal(t, addr.Mask, net.IPv4Mask(255, 255, 255, 255))

	assert.Equal(t, mustParseNetAddress("192.0.2.73/32"), addr)
}

func TestParseIPv4WithMask(t *testing.T) {
	addr, err := parseNetAddress("192.0.2.64/26")
	assert.NoError(t, err)
	assert.True(t, addr.IP.Equal(net.IPv4(192, 0, 2, 64)))
	assert.Equal(t, addr.Mask, net.IPv4Mask(255, 255, 255, 192))
}

func TestParseIPv6(t *testing.T) {
	addr, err := parseNetAddress("2001:db8::ff00")
	assert.NoError(t, err)

	expected := net.ParseIP("2001:db8::ff00")
	assert.NotNil(t, expected)

	assert.True(t, addr.IP.Equal(expected))
	assert.Equal(t, addr.Mask, net.CIDRMask(128, 128))

	assert.Equal(t, mustParseNetAddress("2001:db8::ff00/128"), addr)
}

func TestParseIPv6WithMask(t *testing.T) {
	addr, err := parseNetAddress("2001:db8:ff::/64")
	assert.NoError(t, err)

	expected := net.ParseIP("2001:db8:ff::")
	assert.NotNil(t, expected)

	assert.True(t, addr.IP.Equal(expected))
	assert.Equal(t, addr.Mask, net.CIDRMask(64, 128))
}

func TestParseEmpty(t *testing.T) {
	_, err := parseNetAddress("")
	assert.Error(t, err)
	assert.Equal(t, err.Error(), `Parsing IP address "" failed`)
}

func TestMarshalAddress(t *testing.T) {
	addr, err := parseNetAddress("2001:db8:ff::/64")
	assert.NoError(t, err)

	m, err := addr.MarshalText()
	assert.Equal(t, m, []byte("2001:db8:ff::/64"))
}
