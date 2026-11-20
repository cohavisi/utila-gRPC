.PHONY: lint test build proto install-hooks

test:
	go test -race ./...
	go vet ./...

lint:
	@files="$$(gofmt -l cmd internal integration api)" || exit $$?; \
	if [ -n "$$files" ]; then \
		printf 'Go files need gofmt:\n%s\n' "$$files"; \
		exit 1; \
	fi

build:
	CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server
	CGO_ENABLED=0 go build -trimpath -o bin/client ./cmd/client

# Install protoc 36.2 separately; plugin versions are pinned below.
proto:
	mkdir -p .tools
	GOBIN="$(CURDIR)/.tools" go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
	GOBIN="$(CURDIR)/.tools" go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.1
	PATH="$(CURDIR)/.tools:$$PATH" protoc --go_out=. --go_opt=module=github.com/cohavisi/utila-gRPC --go-grpc_out=. --go-grpc_opt=module=github.com/cohavisi/utila-gRPC api/greeter/v1/greeter.proto

install-hooks:
	git config --local core.hooksPath .githooks
