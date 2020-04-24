# /************************************************************************
 #  File: Makefile
 #  Author: mdr0id
 #  Date: 7/16/2019
 #  Description:  Used for local and container dev in CI deployments
 #  Usage: make <target_name>
 #
 #  Copyright (c) 2020 The Zcash developers
 #  Distributed under the MIT software license, see the accompanying
 #  file COPYING or https://www.opensource.org/licenses/mit-license.php .
 #
 #  Known bugs/missing features:
 #  1. make msan is not stable as of 9/20/2019
 #
 # ************************************************************************/
PROJECT_NAME := "lightwalletd"
GO_FILES := $(shell find . -name '*.go' | grep -v /vendor/ | grep -v '*_test.go')
GO_TEST_FILES := $(shell find . -name '*_test.go' -type f | rev | cut -d "/" -f2- | rev | sort -u)
GO_BUILD_FILES := $(shell find . -name 'main.go')

VERSION := `git describe --tags`
GITCOMMIT := `git rev-parse HEAD`
BUILDDATE := `date +%Y-%m-%d`
BUILDUSER := `whoami`
LDFLAGSSTRING :=-X github.com/zcash/lightwalletd/common.Version=$(VERSION)
LDFLAGSSTRING +=-X github.com/zcash/lightwalletd/common.GitCommit=$(GITCOMMIT)
LDFLAGSSTRING +=-X github.com/zcash/lightwalletd/common.Branch=$(BRANCH)
LDFLAGSSTRING +=-X github.com/zcash/lightwalletd/common.BuildDate=$(BUILDDATE)
LDFLAGSSTRING +=-X github.com/zcash/lightwalletd/common.BuildUser=$(BUILDUSER)
LDFLAGS :=-ldflags "$(LDFLAGSSTRING)"

# There are some files that are generated but are also in source control
# (so that the average clone - build doesn't need the required tools)
GENERATED_FILES := docs/rtd/index.html walletrpc/compact_formats.pb.go walletrpc/service.pb.go walletrpc/darkside.proto

PWD := $(shell pwd)

.PHONY: all dep build clean test coverage lint doc simpledoc

all: first-make-timestamp build $(GENERATED_FILES)

# Ensure that the generated files that are also in git source control are
# initially more recent than the files they're generated from (so we don't try
# to rebuild them); this isn't perfect because it depends on doing a make before
# editing a .proto file; also, "make -jn" may trigger remake if n > 1.
first-make-timestamp:
	touch $(GENERATED_FILES) $@

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

docs/rtd/index.html: walletrpc/compact_formats.proto walletrpc/service.proto walletrpc/darkside.proto
	docker run --rm -v $(PWD)/docs/rtd:/out -v $(PWD)/walletrpc:/protos pseudomuto/protoc-gen-doc

walletrpc/service.pb.go: walletrpc/service.proto
	cd walletrpc && protoc service.proto --go_out=plugins=grpc:.

walletrpc/darkside.pb.go: walletrpc/darkside.proto
	cd walletrpc && protoc darkside.proto --go_out=plugins=grpc:.

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
	GO111MODULE=on go build $(LDFLAGS) 

build_rel:
	GO111MODULE=on GOOS=linux go build $(LDFLAGS) 

# Install binaries into Go path
install:
	go install ./...

# Update your protoc, protobufs, grpc, .pb.go files
update-grpc:
	go get -u github.com/golang/protobuf/proto
	go get -u github.com/golang/protobuf/protoc-gen-go
	go get -u google.golang.org/grpc
	cd walletrpc && protoc service.proto --go_out=plugins=grpc:.
	cd walletrpc && protoc darkside.proto --go_out=plugins=grpc:.
	go mod tidy && go mod vendor

clean:
	@echo "clean project..."
	#rm -f $(PROJECT_NAME)
