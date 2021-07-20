# The main binary is statically linked, but it requires access to the TLS root certificates to verify connections.
FROM docker.io/library/alpine:3.14

ENTRYPOINT ["/usr/bin/floaty"]

RUN apk add --no-cache ca-certificates

COPY floaty /usr/bin/floaty
USER 1000:0
