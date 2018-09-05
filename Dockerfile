FROM docker.io/library/golang:1.11 AS ursula-builder

ARG DEP_VERSION=0.5.0

RUN \
  curl --location --output "$GOPATH/bin/dep" \
    "https://github.com/golang/dep/releases/download/v${DEP_VERSION}/dep-linux-amd64" && \
  chmod +x "$GOPATH/bin/dep"

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

WORKDIR /go/src/git.vshn.net/appuio-public/ursula

# Pre-build dependencies
COPY Gopkg.toml Gopkg.lock ./
RUN dep ensure -v -vendor-only
RUN go build -v ./vendor/...

ARG CI_COMMIT_REF_NAME=
ARG CI_COMMIT_SHA=

# Build main code
COPY --chown=0:0 [".", "/go/src/git.vshn.net/appuio-public/ursula"]
RUN \
  set -e && \
  packages=$(go list ./...) || exit 1; \
  echo packages; \
  echo "$packages" | xargs; \
  echo test; \
  echo "$packages" | CGO_ENABLED=1 xargs --no-run-if-empty go test -race
RUN \
  set -e; \
  ldflags='-extldflags "-static"' && \
  ldflags="${ldflags} -X 'main.commitRefName=${CI_COMMIT_REF_NAME}'" && \
  ldflags="${ldflags} -X 'main.commitSHA=${CI_COMMIT_SHA}'" && \
  exec go build -v -tags netgo -ldflags "$ldflags" -o /tmp/ursula .

# The main binary is statically linked, but it requires access to the TLS root
# certificates to verify connections.
FROM docker.io/library/alpine:latest

RUN apk add --no-cache tini ca-certificates

COPY --from=ursula-builder --chown=0:0 /tmp/ursula /bin/ursula

ENTRYPOINT ["/sbin/tini", "--", "/bin/ursula"]

# vim: set sw=2 sts=2 et :
