# /************************************************************************
 #  File: Dockerfile
 #  Author: mdr0id
 #  Date: 9/3/2019
 #  Description:  Used for devs that have not built zcash or lightwalletd on
 #                on existing system
 #  USAGE:
 #
 #  To build image: make docker_img
 #  To run container: make docker_image_run
 #  
 #  This will place you into the container where you can run zcashd, zcash-cli, 
 #  lightwalletd ingester, and lightwalletd server etc..
 #
 #  First you need to get zcashd sync to current height on testnet, from outside container:
 #  make docker_img_run_zcashd
 #
 #  Sometimes you need to manually start zcashd for the first time, from insdie the container:
 #  zcashd -printtoconsole   
 #
 #  Once the block height is atleast 280,000 you can go ahead and start lightwalletd components
 #  make docker_img_run_lightwalletd_ingest
 #  make docker_img_run_lightwalletd_insecure_server
 #  
 #  If you need a random bash session in the container, use:
 #  make docker_img_bash
 #
 #  If you get kicked out of docker or it locks up...
 #  To restart, check to see what container you want to restart via docker ps -a
 #  Then, docker restart <container id>
 #  The reattach to it, docker attach <container id>
 #
 #  Known bugs/missing features/todos:
 #
 #  *** DO NOT USE IN PRODUCTION ***
 #  
 #  - Create docker-compose with according .env scaffolding 
 #  - Determine librustzcash bug that breaks zcashd alpine builds at runtime
 #  - Once versioning is stable add config flags for images
 #  - Add mainnet config once lightwalletd stack supports it 
 #
 # ************************************************************************/

# Create layer in case you want to modify local lightwalletd code
FROM golang:1.11 AS lightwalletd_base

ENV ZCASH_CONF=/root/.zcash/zcash.conf
ENV LIGHTWALLETD_URL=https://github.com/zcash-hackworks/lightwalletd.git

RUN apt-get update && apt-get install make git gcc
WORKDIR /home

# Comment out line below to use local lightwalletd repo changes
RUN git clone ${LIGHTWALLETD_URL}

# To add local changes to container uncomment this line
#ADD . /home

RUN cd ./lightwalletd && make
RUN /usr/bin/install -c /home/lightwalletd/ingest /home/lightwalletd/server /usr/bin/

# Setup layer for zcashd and zcash-cli binary
FROM golang:1.11 AS zcash_builder

ENV ZCASH_URL=https://github.com/zcash/zcash.git

RUN apt-get update && apt-get install \
    build-essential pkg-config libc6-dev m4 g++-multilib \
    autoconf libtool ncurses-dev unzip git python python-zmq \
    zlib1g-dev wget curl bsdmainutils automake python-pip -y

WORKDIR /build
RUN git clone ${ZCASH_URL}

RUN ./zcash/zcutil/build.sh -j$(nproc)
RUN bash ./zcash/zcutil/fetch-params.sh
RUN /usr/bin/install -c /build/zcash/src/zcashd /build/zcash/src/zcash-cli /usr/bin/

# Create layer for lightwalletd and zcash binaries to reduce image size
FROM golang:1.11 AS zcash_runner

ARG ZCASH_VERSION=2.0.7+3
ARG ZCASHD_USER=zcash
ARG ZCASHD_UID=1001
ARG ZCASH_CONF=/home/$ZCASHD_USER/.zcash/zcash.conf

RUN useradd -s /bin/bash -u $ZCASHD_UID $ZCASHD_USER

RUN mkdir -p /home/$ZCASHD_USER/.zcash/ && \
    mkdir -p /home/$ZCASHD_USER/.zcash-params/ && \
    chown -R $ZCASHD_USER /home/$ZCASHD_USER/.zcash/ && \
    mkdir /logs/ && \
    mkdir /db/

USER $ZCASHD_USER
WORKDIR /home/$ZCASHD_USER/

# Use lightwallet server and ingest binaries from prior layer
COPY --from=lightwalletd_base /usr/bin/ingest /usr/bin/server /usr/bin/
COPY --from=zcash_builder /usr/bin/zcashd /usr/bin/zcash-cli /usr/bin/
COPY --from=zcash_builder /root/.zcash-params/ /home/$ZCASHD_USER/.zcash-params/

# Configure zcash.conf
RUN echo "testnet=1" >> ${ZCASH_CONF} && \
    echo "addnode=testnet.z.cash" >> ${ZCASH_CONF} && \
    echo "rpcbind=127.0.0.1" >> ${ZCASH_CONF} && \
    echo "rpcport=18232" >> ${ZCASH_CONF} && \
    echo "rpcuser=lwd" >> ${ZCASH_CONF} && \
    echo "rpcpassword=`head /dev/urandom | tr -dc A-Za-z0-9 | head -c 13 ; echo ''`" >> ${ZCASH_CONF}
 
VOLUME [/home/$ZCASH_USER/.zcash]
VOLUME [/home/$ZCASH_USER/.zcash-params]
