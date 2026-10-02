.PHONY: generate build run test test-e2e migrate lint fmt

OAPI_CODEGEN := $(shell go env GOPATH)/bin/oapi-codegen
SQLC         := $(shell go env GOPATH)/bin/sqlc

# Install codegen tools locally
tools:
	go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

# Generate all code from specs
generate:
	mkdir -p internal/api internal/db/sqlc
	$(OAPI_CODEGEN) -generate "chi-server,types,strict-server,spec" -package api -o internal/api/api.gen.go api/openapi.yaml
	$(SQLC) generate

# Build the binary
build: generate
	go build -o bin/server ./cmd/server

# Run via docker compose
run:
	docker compose up --build -d

# Run unit tests (exclude e2e)
test:
	go test $$(go list ./... | grep -v /e2e) -v -count=1

# Run end-to-end tests (requires Docker)
test-e2e:
	go test ./internal/e2e/... -v -count=1 -timeout 30s

# Apply migrations (requires running DB)
migrate:
	go run ./cmd/migrate/main.go

# Tidy dependencies
tidy:
	go mod tidy

# Format code using golangci-lint formatter (goimports)
fmt:
	golangci-lint fmt ./...

# Lint code
lint:
	golangci-lint run ./...

# Remove generated files
clean:
	rm -rf internal/api/ internal/db/sqlc/ bin/
