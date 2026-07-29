FROM golang:1.25-alpine AS builder

RUN apk add --no-cache make git

WORKDIR /go/src/github.com/zcash/lightwalletd

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN make build

FROM alpine:3.21

RUN apk add --no-cache ca-certificates \
    && adduser -D -h /srv/lightwalletd -u 2002 lightwalletd \
    && mkdir -p /var/lib/lightwalletd/db \
    && chown lightwalletd:lightwalletd /var/lib/lightwalletd/db

COPY --from=builder /go/src/github.com/zcash/lightwalletd/lightwalletd /usr/local/bin/

# Run unprivileged. Existing deployments whose data directory was written by
# an earlier root-running image must chown it to 2002:2002 (or run the
# container with --user 0:0) before upgrading; see CHANGELOG.md.
USER lightwalletd
WORKDIR /srv/lightwalletd

ENTRYPOINT ["lightwalletd"]
CMD ["--help"]
