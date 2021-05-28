FROM docker.io/library/golang:1.16 AS floaty-builder

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

WORKDIR /go/src/floaty

# Pre-build dependencies
COPY go.* .
RUN go mod download

ARG CI_COMMIT_REF_NAME=
ARG CI_COMMIT_SHA=

# Build main code
COPY --chown=0:0 [".", "/go/src/floaty"]
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
  ldflags="${ldflags} -s -w" && \
  ldflags="${ldflags} -X 'main.commitRefName=${CI_COMMIT_REF_NAME}'" && \
  ldflags="${ldflags} -X 'main.commitSHA=${CI_COMMIT_SHA}'" && \
  exec go build -v -tags netgo -ldflags "$ldflags" -o /tmp/floaty .

# The main binary is statically linked, but it requires access to the TLS root
# certificates to verify connections.
FROM docker.io/library/alpine:latest

RUN apk add --no-cache tini ca-certificates

COPY --from=floaty-builder --chown=0:0 /tmp/floaty /bin/floaty

ENTRYPOINT ["/sbin/tini", "--", "/bin/floaty"]

# vim: set sw=2 sts=2 et :
