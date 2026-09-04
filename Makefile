.PHONY: build test test-race lint fmt install

build:
	go build -trimpath -o contextmatrix-setup .

install:
	go install .

test:
	go test ./...

test-race:
	go test -race -count=1 ./...

test-integration:
	go test -tags integration -count=1 ./internal/integration/...

lint:
	golangci-lint run ./...

fmt:
	gofumpt -w .
