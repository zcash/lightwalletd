build:
	(cd cmd/ingest ; go build)
	(cd cmd/server ; go build)

fmt:
	git ls-files -- '*.go' | xargs gofmt -w

test: build
	(cd parser ; go test)
	(cd storage ; go test)
