.PHONY: test build proto

test:
	go test -race ./...
	go vet ./...

build:
	CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server
	CGO_ENABLED=0 go build -trimpath -o bin/client ./cmd/client

# Install protoc 36.2 separately; plugin versions are pinned below.
proto:
	mkdir -p .tools
	GOBIN="$(CURDIR)/.tools" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
	GOBIN="$(CURDIR)/.tools" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1
	PATH="$(CURDIR)/.tools:$$PATH" protoc --go_out=. --go_opt=module=github.com/cohavisi/utila-gRPC --go-grpc_out=. --go-grpc_opt=module=github.com/cohavisi/utila-gRPC api/greeter/v1/greeter.proto
