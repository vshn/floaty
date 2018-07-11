package main

import (
	"fmt"
	"net"
)

type netAddress struct {
	net.IPNet
}

func (netaddr *netAddress) UnmarshalText(text []byte) error {
	raw := string(text)

	// Parse CIDR notation IP address and prefix length, like "192.0.2.0/24" or
	// "2001:db8::/32"
	ip, ipnet, err := net.ParseCIDR(raw)
	if err == nil {
		netaddr.IP = ipnet.IP
		netaddr.Mask = ipnet.Mask
	} else {
		// Attempt to parse without prefix length
		ip = net.ParseIP(raw)
		if ip == nil {
			return fmt.Errorf("Parsing IP address %q failed", raw)
		}

		netaddr.IP = ip

		if ip = netaddr.IP.To4(); ip == nil {
			ip = netaddr.IP.To16()
		}

		if ip == nil {
			return fmt.Errorf("Unknown IP address type")
		}

		netaddr.IP = ip
		netaddr.Mask = net.CIDRMask(8*len(ip), 8*len(ip))
	}

	return nil
}

func parseNetAddress(text string) (netAddress, error) {
	result := netAddress{}

	if err := result.UnmarshalText([]byte(text)); err != nil {
		return result, err
	}

	return result, nil
}

func mustParseNetAddress(text string) netAddress {
	result := netAddress{}

	if err := result.UnmarshalText([]byte(text)); err != nil {
		panic(err)
	}

	return result
}
