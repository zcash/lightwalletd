# /************************************************************************
 #  File: Makefile
 #  Author: mdr0id
 #  Date: 7/16/2019
 #  Description:  Used for local and container dev in CI deployments
 #  Usage: make <target_name>
 #
 #  Known bugs/missing features:
 #  1. make msan is not stable as of 9/20/2019
 #
 # ************************************************************************/
PROJECT_NAME := "lightwalletd"
GO_FILES := $(shell find . -name '*.go' | grep -v /vendor/ | grep -v '*_test.go')
GO_TEST_FILES := $(shell find . -name '*_test.go' -type f | rev | cut -d "/" -f2- | rev | sort -u)
GO_BUILD_FILES := $(shell find . -name 'main.go')

.PHONY: all dep build clean test coverage coverhtml lint

all: build

# Lint golang files
lint:
	@golint -set_exit_status

show_tests:
	@echo ${GO_TEST_FILES}

# Run unittests
test:
	@go test -v -coverprofile=coverage.txt -covermode=atomic ./...

# Run data race detector
race:
	GO111MODULE=on CGO_ENABLED=1 go test -v -race -short ./...

# Run memory sanitizer (need to ensure proper build flag is set)
msan:
	@go test -v -msan -short ${GO_TEST_FILES}

# Generate global code coverage report
coverage:
	@go test -coverprofile=coverage.out -covermode=atomic ./...

# Generate code coverage report
coverage_report:
	@go tool cover -func=coverage.out 

# Generate code coverage report in HTML
coverage_html: 
	@go tool cover -html=coverage.out -o coverage.html

# Generate documents
docs:
	@echo "Generating docs..."

# Generate docker image
docker_img:
	docker build -t zcash_lwd_base .

# Run the above docker image in a container
docker_img_run:
	docker run -i --name zcashdlwd zcash_lwd_base

# Execture a bash process on zcashdlwdcontainer
docker_img_bash:
	docker exec -it zcashdlwd bash

# Start the zcashd process in the zcashdlwd container
docker_img_run_zcashd:
	docker exec -i zcashdlwd zcashd -printtoconsole

# Stop the zcashd process in the zcashdlwd container
docker_img_stop_zcashd:
	docker exec -i zcashdlwd zcash-cli stop

# Start the lightwalletd ingester in the zcashdlwd container 
docker_img_run_lightwalletd_ingest:
	docker exec -i zcashdlwd ingest --conf-file /root/.zcash/zcash.conf --db-path /db/sql.db --log-file /logs/ingest.log

# Start the lightwalletd server in the zcashdlwd container
docker_img_run_lightwalletd_insecure_server:
	docker exec -i zcashdlwd server --very-insecure=true --conf-file /root/.zcash/zcash.conf --db-path /db/sql.db --log-file /logs/server.log --bind-addr 127.0.0.1:18232

# Remove and delete ALL images and containers in Docker; assumes containers are stopped
docker_remove_all:
	docker system prune -f

# Get dependencies
dep:
	@go get -v -d ./...

# Build binary
build:
	GO111MODULE=on CGO_ENABLED=1 go build -i -v ./cmd/ingest
	GO111MODULE=on CGO_ENABLED=1 go build -i -v ./cmd/server

# Install binaries into Go path
install:
	go install ./...

clean:
	@echo "clean project..."
	#rm -f $(PROJECT_NAME)