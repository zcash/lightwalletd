# /************************************************************************
 #  File: Makefile
 #  Author: mdr0id
 #  Date: 7/16/2019
 #  Description:  Used for local and container dev in CI deployments
 #  Usage: make <target_name>
 #
 #  Known bugs/missing features:
 #
 # ************************************************************************/
PROJECT_NAME := "lightwalletd"
GO_FILES := $(shell find . -name '*.go' | grep -v /vendor/ | grep -v '*_test.go')
GO_TEST_FILES := $(shell find . -name '*_test.go' -type f | rev | cut -d "/" -f2- | rev | sort -u)
GO_BUILD_FILES := $(shell find . -name 'main.go')

#PKG_LIST := $(shell go list | grep -v /vendor/)
#GO_COV_REPORTS :=

.PHONY: all dep build clean test coverage coverhtml lint

all: build

# Lint golang files
lint:
	@golint -set_exit_status

show_tests:
	@echo ${GO_TEST_FILES}

# Run unittests
test:
	@go test -v -short ${GO_TEST_FILES}

# Run data race detector
race: dep
	@go test -v -race -short ${GO_TEST_FILES}

# Run memory sanitizer (need to ensure proper build flag is set)
msan: dep
	@go test -v -msan -short ${GO_TEST_FILES}

# Generate global code coverage report
coverage:
	@go test -coverprofile=coverage.out ${GO_TEST_FILES}

# Generate code coverage report
coverage_report:
	@go tool cover -func=coverage.out 

# Generate code coverage report in HTML
coverage_html: 
	@go tool cover -html=coverage.out

# Generate documents
docs:
	@echo "Generating docs..."

# Generate docker image
image:
	@echo "Building lightwalletd image..."

# Get dependencies
dep:
	@go get -v -d ./...

# Build binary
build: #dep
	@go build -i -v ${GO_BUILD_FILES}

# Install binaries into Go path
install:
	go install ./...

clean:
	@echo "clean project..."
	#rm -f $(PROJECT_NAME)