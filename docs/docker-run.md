# Docker images
Docker images are available on Docker Hub at [electriccoinco/lightwalletd](https://hub.docker.com/repository/docker/electriccoinco/lightwalletd).

## Using command line options

Already have a zcash node running with an exposed RPC endpoint?

Try the docker container with command lines flags like:
```
docker run --rm -p 9067:9067 \
  electriccoinco/lightwalletd:v0.4.2 \
  --grpc-bind-addr 0.0.0.0:9067 \
  --no-tls-very-insecure \
  --rpchost 192.168.86.46 \
  --rpcport 38237 \
  --rpcuser zcashrpc \
  --rpcpassword notsecure \
  --log-file /dev/stdout
```

## Preserve the compactblocks database between runs

Like the first example, but this will preserve the lightwalletd compactblocks database for use between runs.


Create a directory somewhere and change the `uid` to `2002`.  
The is the id of the restricted lightwalletd user inside of the container.  

```
mkdir ./lightwalletd_db_volume
sudo chown 2002 ./lightwalletd_db_volume
```

Now add a `--volume` mapping from the local file path to where we want it to show up inside the container.

Then, add the `--data-dir` to the lightwalletd command with the value of path mapping as viewed from inside the container.

```
docker run --rm -p 9067:9067 \
  --volume $(pwd)/lightwalletd_db_volume:/srv/lightwalletd/db_volume \
  electriccoinco/lightwalletd:v0.4.2 \
  --grpc-bind-addr 0.0.0.0:9067 \
  --no-tls-very-insecure \
  --rpchost 192.168.86.46 \
  --rpcport 38237 \
  --rpcuser zcashrpc \
  --rpcpassword notsecure \
  --data-dir /srv/lightwalletd/db_volume \
  --log-file /dev/stdout
```


## Using a YAML config file

When using a configuration file with the docker image, you must create the configuration file and then map it into the container. Finally, provide a command line option referencing the mapped file location.

Create a configuration file:
```
cat <<EOF >lightwalletd.yml
no-tls-very-insecure: true
log-file: /dev/stdout
rpcuser: zcashrpc
rpcpassword: notsecure
rpchost: 192.168.86.46
rpcport: 38237
grpc-bind-addr: 0.0.0.0:9067
EOF
```

Use it with the docker container
```
docker run  --rm \
    -p 9067:9067 \
    -v $(pwd)/lightwalletd.yml:/tmp/lightwalletd.yml \
    electriccoinco/lightwalletd:v0.4.2 \
    --config /tmp/lightwalletd.yml
```

## Using docker-compose for a full stack

Don't have an existing zcash node? Check out the [docker-compose](./docker-compose-setup.md) for examples of multi cantainer usage.
