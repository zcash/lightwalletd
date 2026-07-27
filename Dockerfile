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

# NB: the container still runs as root, matching the previous image. The
# lightwalletd user exists and owns the db directory, but switching to it
# would break existing deployments whose data directories were written as
# root, so that change is deliberately left separate.
WORKDIR /srv/lightwalletd

ENTRYPOINT ["lightwalletd"]
CMD ["--help"]
