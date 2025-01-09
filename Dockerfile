ARG APP_HOME=/srv/lightwalletd
ARG ZCASHD_CONF_PATH=$APP_HOME/zcash.conf

##
## Builder
##
# Create layer in case you want to modify local lightwalletd code
FROM golang:1.22 AS build


# Create and change to the app directory.
WORKDIR /app

# Retrieve application dependencies.
# This allows the container build to reuse cached dependencies.
# Expecting to copy go.mod and if present go.sum.
COPY go.mod ./
COPY go.sum ./

# Do not use `go get` as it updates the requirements listed in your go.mod file.
# `go mod download` does not add new requirements or update existing requirements.
RUN go mod download

# Copy local code to the container image.
COPY . ./

# Build and install the binary.
RUN go build -v -o /usr/local/bin/lightwalletd

##
## Runtime
##
FROM debian:bookworm-slim as runtime

# Get the needed ARGs values
ARG APP_HOME
ARG ZCASHD_CONF_PATH
ARG LWD_GRPC_PORT=9067
ARG LWD_HTTP_PORT=9068
ARG LWD_USER=lightwalletd
ARG LWD_UID=2002

# Always run a container with a non-root user. Running as root inside the container is running as root in the Docker host
# If an attacker manages to break out of the container, they will have root access to the host
RUN groupadd --gid ${LWD_UID} ${LWD_USER}  && \
    useradd --home-dir ${APP_HOME} --no-create-home --gid ${LWD_USER} --uid ${LWD_UID} --shell /bin/sh ${LWD_USER}

# Create the directory for the database and certificates to keep backwards compatibility
RUN mkdir -p /var/lib/lightwalletd/db && \
    mkdir -p /secrets/lightwallted && \
    chown ${LWD_USER}:${LWD_USER} /var/lib/lightwalletd/db && \
    chown ${LWD_USER}:${LWD_USER} /secrets/lightwallted

WORKDIR ${APP_HOME}

COPY --from=build /usr/local/bin/lightwalletd /usr/local/bin
COPY ./docker/cert.key ./
COPY ./docker/cert.pem ./

RUN set -ex; \
  { \
    echo "rpcuser=zcashrpc"; \
    echo "rpcpassword=`head /dev/urandom | tr -dc A-Za-z0-9 | head -c 13 ; echo ''`" \
    echo "rpcbind=zcashd"; \
    echo "rpcport=3434"; \
  } > "${ZCASHD_CONF_PATH}"

EXPOSE ${LWD_GRPC_PORT}
EXPOSE ${LWD_HTTP_PORT}

USER ${LWD_USER}

ENTRYPOINT ["lightwalletd"]
CMD ["--tls-cert=cert.pem", "--tls-key=cert.key", "--grpc-bind-addr=0.0.0.0:9067",  "--http-bind-addr=0.0.0.0:9068", "--log-file=/dev/stdout", "--log-level=7"]
