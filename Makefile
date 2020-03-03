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

PWD := $(shell pwd)

.PHONY: all dep build clean test coverage coverhtml lint doc simpledoc

all: build

# Lint golang files
lint:
	golint -set_exit_status

show_tests:
	@echo ${GO_TEST_FILES}

# Run unittests
test:
	go test -v ./...

# Run data race detector
race:
	GO111MODULE=on CGO_ENABLED=1 go test -v -race -short ./...

# Run memory sanitizer (need to ensure proper build flag is set)
msan:
	go test -v -msan -short ${GO_TEST_FILES}

# Generate global code coverage report, ignore generated *.pb.go files

coverage:
	go test -coverprofile=coverage.out ./...
	sed -i '/\.pb\.go/d' coverage.out

# Generate code coverage report
coverage_report: coverage
	go tool cover -func=coverage.out 

# Generate code coverage report in HTML
coverage_html: coverage
	go tool cover -html=coverage.out

# Generate documents, requires docker, see https://github.com/pseudomuto/protoc-gen-doc
doc: docs/rtd/index.html

docs/rtd/index.html: walletrpc/compact_formats.proto walletrpc/service.proto
	docker run --rm -v $(PWD)/docs/rtd:/out -v $(PWD)/walletrpc:/protos pseudomuto/protoc-gen-doc

# Generate documents using a very simple wrap-in-html approach (not ideal)
simpledoc: lwd-api.html

lwd-api.html: walletrpc/compact_formats.proto walletrpc/service.proto
	./docgen.sh $^ >lwd-api.html

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

# Start the lightwalletd server in the zcashdlwd container
docker_img_run_lightwalletd_insecure_server:
	docker exec -i zcashdlwd server --no-tls-very-insecure=true --conf-file /home/zcash/.zcash/zcash.conf --log-file /logs/server.log --bind-addr 127.0.0.1:18232

# Remove and delete ALL images and containers in Docker; assumes containers are stopped
docker_remove_all:
	docker system prune -f

# Get dependencies
dep:
	@go get -v -d ./...

# Build binary
build:
	GO111MODULE=on CGO_ENABLED=1 go build -i -v ./cmd/server

build_rel:
	GO111MODULE=on CGO_ENABLED=1 GOOS=linux go build -ldflags="-s -w" -i -v ./cmd/server

# Install binaries into Go path
install:
	go install ./...

clean:
	@echo "clean project..."
	#rm -f $(PROJECT_NAME)
