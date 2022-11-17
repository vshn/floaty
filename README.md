# Floaty

[![](https://img.shields.io/github/workflow/status/vshn/floaty/Release)](https://github.com/vshn/floaty/actions)
[![](https://img.shields.io/github/v/release/vshn/floaty)](https://github.com/vshn/floaty/releases)
![](https://img.shields.io/github/go-mod/go-version/vshn/floaty)
[![](https://img.shields.io/github/downloads/vshn/floaty/total)](https://github.com/vshn/floaty/releases)
[![](https://img.shields.io/badge/container-quay.io-green)](https://quay.io/repository/vshn/floaty?tab=tags)
[![](https://img.shields.io/github/license/vshn/floaty)](https://github.com/vshn/floaty/blob/master/LICENSE)

**Cloud provider API integration for Keepalived**

Floaty is a program suitable for use by a Keepalived notification script. By
regularily enforcing the destination of a floating or elastic IP address it
ensures that split-brain situations or external modifications to the
destination have little impact.

For VRRP instances in `MASTER` status Floaty reads the Keepalived
configuration. When no addresses are configured in the Floaty configuration
the IP addresses assigned to the given VRRP instance are managed.
Managing an IP address means to refresh its target host via a provider-specific
API in a regular interval. Failures are handled gracefully.

For instances in `BACKUP` and `FAULT` status any existing instance for
a previous `MASTER` status is terminated by sending a signal and waiting for
its termination.

Logs are written to standard error.


## Command line flags

* `--verbose`: Produce more log messages. May also be enabled by setting the
  `FLOATY_LOG_VERBOSE` environment variable to a non-empty value.

* `--json-log`: Output log messages in JSON format for further processing.

* `--dry-run`: Updates to Floating IPs are only logged and not performed.


## Configuration

Configuration data must be supplied as a YAML file. The top level is a map.

* `lock-file-template`: Template for path to lock file for each VRRP instance.
  Must contain a single `%s` to be replaced by VRRP instance name. Defaults to
  `/var/lock/floaty-%s.lock`.

* `lock-timeout`: How long to wait for lock as a duration. Defaults to 10
  seconds.

* `keepalived-config`: Path to Keepalived configuration. Defaults to
  `/etc/keepalived/keepalived.conf`. The configuration is parsed to verify
  the existence of the VRRP instance name given on the command line. If
  `managed-addresses` is not used the IP addresses assigned to the VRRP
  instance are used.

* `managed-addresses`: Array with IP addresses to manage.

* `refresh-interval`: How long to wait between refreshes of individual
  addresses as a duration. Defaults to 1 minute. Minimal jitter is
  automatically added to avoid the thundering herd problem.

* `refresh-timeout`: How long refreshing an individual address may take at most
  as a duration. Defaults to 10 seconds. Refreshes are parallelized and do not
  wait for each other.

* `back-off`: A map configuring the back-off behaviour for retries of failing
  address refreshes. Jitter is automatically added to avoid the thundering herd
  problem.

  * `initial-interval`: How long to wait for the first retry as a duration.
    Defaults to 1 second.
  * `multiplier`: Multiply backoff period by given number before retries.
    Defaults to 1.1.
  * `max-interval`: Maximum duration of backoff period. Defaults to 10 seconds.
  * `max-elapsed-time`: Give up on retries and revert to normal interval after
    given amount of time. Defaults to zero for infinite retries.

* `provider`: Cloud API provider, must be either `cloudscale` or `exoscale`.
  Provider-specific settings are in separate keys.

* `cloudscale`: Cloudscale.ch-specific settings as a map. When neither
  `server-uuid` nor `hostname-to-server-uuid` is specified a metadata service
  is used to automatically discover the instance UUID of a server.

  * `endpoint`: URL for API endpoint. Defaults to production URL.
  * `token`: API authentication token as a string. Must have write access.
  * `server-uuid`: UUID of next-hop server for IP address(es). Overrides
    `hostname-to-server-uuid` if both are given.
  * `hostname-to-server-uuid`: Map with hostname as key and next-hop server
    UUID as value. Hostname as reported by kernel is used for lookup.

* `exoscale`: Exoscale-specific settings as a map.

  * `endpoint`: URL for API endpoint. Defaults to production URL.
  * `key`: API access key (starts with `EXO`).
  * `secret`: API access secret.
  * `instance-id`: Virtual machine ID as string; if not given a metadata
    service is used to automatically retrieve the ID of the machine running the
    program.


### Hostnames

Hostnames used in the configuration must match the kernel's hostname as
returned by Go's [`os.Hostname` function](https://golang.org/pkg/os/#Hostname).
Depending on the system the name may or may not be a fully qualified domain
name (FQDN).

On Linux systems the `hostname` command without parameters will return the
configured name which is also retrievable from the `/proc/sys/kernel/hostname`
pseudo file.


## Example configuration

The configuration shown is for demonstration, not a recommendation.

```
lock-file-template: "/var/run/floaty.%s.lock"

refresh-interval: 2m

back-off:
  max-elapsed-time: 30s

provider: cloudscale
cloudscale:
  token: HELPIMTRAPPEDINATOKENGENERATOR
  hostname-to-server-uuid:
    lb1.example.net: 96defb88-002c-4985-b795-5c929bab23da
    lb2.example.net: 7d37a073-e84c-4fc6-b631-cc2e29d9d4ea
    lb3.example.net: e1dfe126-bc14-494e-998f-d221b02941b4

exoscale:
  key: EXOLICIOUS
  secret: NomNomNom

# See description: use only if Keepalived configuration doesn't contain
# addresses
managed-addresses:
  - 192.0.2.1/24
  - 192.0.2.2/24
```


## Usage

Keepalived as of version 1.2.24 always redirects the output of notification
scripts to `/dev/null` and does not support passing custom parameters. Because
floaty requires a configuration file a wrapper needs to be used. In most cases
a shell script is suitable. Example:

```
#!/bin/sh

/usr/bin/floaty /etc/keepalived/floaty-prod.yml "$@" 2>&1 | \
  logger --tag floaty-prod
```


### Docker container

Docker only collects logs from PID 1 (init). As of Docker 1.13 there is no
other way to emit runtime logs than to redirect into the file descriptors of
PID 1.

```
#!/bin/sh

exec /bin/floaty --json-log /etc/floaty.yml "$@" \
  >>/proc/1/fd/1 2>>/proc/1/fd/2
```


### Test mode

Floaty implements provider-specific self-tests which can be run outside of
Keepalived.

```
/bin/floaty --json-log --verbose --test /etc/floaty.yml
```

### FIFO mode

Floaty can run in FIFO mode allowing it to process notification events through a FIFO instead of using notify scrips 

```
/bin/floaty --fifo /etc/floaty.yml /tmp/fifo
```

## External links

* [Time duration parsing in Go](https://golang.org/pkg/time/#ParseDuration),
  e.g. `1m30s`, `35m`, `40s` or `1h5m50s`.

* [Exponential back-off](https://godoc.org/github.com/cenkalti/backoff#ExponentialBackOff)
  used for adding jitter to retries

* [Thundering herd problem](https://en.wikipedia.org/wiki/Thundering_herd_problem)

<!-- vim: set sw=2 sts=2 et : -->
