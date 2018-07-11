package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

type keepalivedConfigVrrpInstance struct {
	Name      string
	Addresses []netAddress
}

type keepalivedConfig struct {
	vrrpInstances map[string]*keepalivedConfigVrrpInstance
}

type keepalivedConfigParser struct {
	cfg              *keepalivedConfig
	curVrrpInstance  *keepalivedConfigVrrpInstance
	parsingAddresses bool
}

func (parser *keepalivedConfigParser) handleLine(fields []string) error {
	if len(fields) >= 2 && fields[0] == "vrrp_instance" {
		name := fields[1]

		if _, ok := parser.cfg.vrrpInstances[name]; ok {
			return fmt.Errorf("Duplicate VRRP instance name %q", name)
		}

		inst := &keepalivedConfigVrrpInstance{
			Name: name,
		}

		parser.curVrrpInstance = inst
		parser.cfg.vrrpInstances[name] = inst

		return nil
	}

	if inst := parser.curVrrpInstance; inst != nil {
		if parser.parsingAddresses {
			if fields[0] == "}" {
				// Block finished
				parser.parsingAddresses = false
				return nil
			}

			addr, err := parseNetAddress(fields[0])
			if err != nil {
				return err
			}

			inst.Addresses = append(inst.Addresses, addr)

			return nil
		}

		if len(fields) > 1 && fields[0] == "virtual_ipaddress" {
			parser.parsingAddresses = true
		}

		return nil
	}

	return nil
}

func (parser *keepalivedConfigParser) handleEOF() error {
	return nil
}

// parseKeepalivedConfig implements a very simplistic, non-robust parser to
// extract VRRP instance configuration blocks in a Keepalived configuration
// file.
func parseKeepalivedConfig(r io.Reader) (*keepalivedConfig, error) {
	parser := keepalivedConfigParser{
		cfg: &keepalivedConfig{
			vrrpInstances: make(map[string]*keepalivedConfigVrrpInstance),
		},
	}

	scanner := bufio.NewScanner(r)
	scanner.Split(bufio.ScanLines)

	linenum := 0

	for scanner.Scan() {
		linenum++

		linetext := scanner.Text()
		trimmed := strings.TrimSpace(linetext)

		// Skip empty lines
		if len(trimmed) == 0 {
			continue
		}

		// Skip comments
		// TODO: Handle end-of-line comments, though care must be taken to
		// handle quoted strings properly
		if trimmed[0] == '#' || trimmed[0] == '!' {
			continue
		}

		fields := strings.Fields(linetext)

		if err := parser.handleLine(fields); err != nil {
			return nil, fmt.Errorf("Line %d: %s", linenum, err)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("Reading configuration failed: %s", err)
	}

	if err := parser.handleEOF(); err != nil {
		return nil, fmt.Errorf("End-of-file: %s", err)
	}

	return parser.cfg, nil
}

func parseKeepalivedConfigFile(path string) (*keepalivedConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	return parseKeepalivedConfig(file)
}
