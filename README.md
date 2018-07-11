# Ursula

**Cloud provider API integration for Keepalived**

Ursula is a program suitable for use by a Keepalived notification script. By
regularily enforcing the destination of a floating or elastic IP address it
ensures that split-brain situations or external modifications to the
destination have little impact.

For VRRP instances in `MASTER` status Ursula reads the Keepalived
configuration, extracts the IP addresses assigned to a given VRRP instance, and
proceeds to refresh those via a provider-specific API in a regular interval.
Failures are handled gracefully.

For instances in `BACKUP` and `FAULT` status any existing instance for
a previous `MASTER` status is terminated by sending a signal and waiting for
its termination.

Logs are written to standard error.


## Command line flags

* `--verbose`: Produce more log messages. May also be enabled by setting the
  `URSULA_LOG_VERBOSE` environment variable to a non-empty value.

* `--json-log`: Output log messages in JSON format for further processing.


## Configuration

Configuration data must be supplied as a YAML file. The top level is a map.

* `lock-file-template`: Template for path to lock file for each VRRP instance.
  Must contain a single `%s` to be replaced by VRRP instance name. Defaults to
  `/var/lock/ursula-%s.lock`.

* `lock-timeout`: How long to wait for lock as a duration. Defaults to 10
  seconds.

* `keepalived-config`: Path to Keepalived configuration. Defaults to
  `/etc/keepalived/keepalived.conf`. The configuration is parsed to extract
  the IP addresses assigned with a VRRP instance.

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

* `cloudscale`: Cloudscale.ch-specific settings as a map.

  * `endpoint`: URL for API endpoint. Defaults to production URL.
  * `token`: API authentication token as a string. Must have write access.
  * `server-uuid`: UUID of next-hop server for IP address(es). Overrides
    `hostname-to-server-uuid` if both are given.
  * `hostname-to-server-uuid`: Map with hostname as key and next-hop server
    UUID as value. Hostname as reported by kernel is used for lookup.


## Example configuration

The configuration shown is for demonstration, not a recommendation.

```
lock-file-template: "/tmp/ursula-%s.lock"

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
```


## Usage

Keepalived as of version 1.2.24 always redirects the output of notification
scripts to `/dev/null` and does not support passing custom parameters. Because
Ursula requires a configuration file a wrapper needs to be used. In most cases
a shell script is suitable. Example:

```
#!/bin/sh

/usr/bin/ursula /etc/keepalived/ursula-prod.yml "$@" 2>&1 | \
  logger --tag ursula-prod
```


### Docker container

Docker only collects logs from PID 1 (init). As of Docker 1.13 there is no
other way to emit runtime logs than to redirect into the file descriptors of
PID 1.

```
#!/bin/sh

exec /bin/ursula --json-log /etc/ursula.yml "$@" \
  >>/proc/1/fd/1 2>>/proc/1/fd/2
```


## External links

* [Time duration parsing in Go](https://golang.org/pkg/time/#ParseDuration),
  e.g. `1m30s`, `35m`, `40s` or `1h5m50s`.

* [Exponential back-off](https://godoc.org/github.com/cenkalti/backoff#ExponentialBackOff)
  used for adding jitter to retries

* [Thundering herd problem](https://en.wikipedia.org/wiki/Thundering_herd_problem)
