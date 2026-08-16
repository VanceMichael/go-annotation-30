GOTOOLCHAIN ?= local
export GOTOOLCHAIN

.PHONY: build test race vet fmt selfcheck docker clean

build:
	go build -o bin/dramactl ./cmd/dramactl

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l .

selfcheck: build
	./bin/dramactl selfcheck

docker:
	docker build -t microdrama:local .

clean:
	rm -rf bin
