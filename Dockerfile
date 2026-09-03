FROM golang:1.25 AS lightwalletd_base

ADD . /go/src/github.com/zcash/lightwalletd
WORKDIR /go/src/github.com/zcash/lightwalletd

# CGO is not needed (no cgo imports), and disabling it yields a static
# binary that depends on nothing in the runtime stage.
ENV CGO_ENABLED=0
RUN make && /usr/bin/install -c ./lightwalletd /usr/local/bin/

FROM debian:13-slim

ARG LWD_USER=lightwalletd
ARG LWD_UID=2002

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates \
  && rm -rf /var/lib/apt/lists/*

RUN useradd --home-dir "/srv/$LWD_USER" \
            --shell /bin/bash \
            --create-home \
            --uid "$LWD_UID" \
            "$LWD_USER" \
  && mkdir -p /var/lib/lightwalletd/db \
  && chown "$LWD_UID:$LWD_UID" /var/lib/lightwalletd/db

COPY --from=lightwalletd_base /usr/local/bin/lightwalletd /usr/local/bin/lightwalletd

WORKDIR "/srv/$LWD_USER"

ENTRYPOINT ["lightwalletd"]
CMD ["--help"]
