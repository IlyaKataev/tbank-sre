COMPOSE ?= docker compose
DOCKER_BUILD_FLAGS ?=
IMAGE ?= tbank-sre
VERSION ?= local
REVISION := $(shell git rev-parse HEAD 2>/dev/null || echo archive)
APP_IMAGE := $(IMAGE):$(VERSION)
OAPI_CODEGEN_VERSION := v2.8.0
SQLC_VERSION := v1.31.1
GOBIN := $(CURDIR)/bin/tools

.PHONY: env build up down logs migrate scale smoke test test-e2e tools generate local-build archive

env:
	python3 scripts/init-env.py

# Build once; up and migrate only run the resulting image.
build:
	docker build $(DOCKER_BUILD_FLAGS) --build-arg VERSION=$(VERSION) --build-arg REVISION=$(REVISION) -t $(APP_IMAGE) .

up:
	APP_IMAGE=$(APP_IMAGE) $(COMPOSE) up -d --wait --wait-timeout 90

down:
	APP_IMAGE=$(APP_IMAGE) $(COMPOSE) down

logs:
	APP_IMAGE=$(APP_IMAGE) $(COMPOSE) logs -f app

migrate:
	APP_IMAGE=$(APP_IMAGE) $(COMPOSE) run --rm migrate

scale:
	APP_IMAGE=$(APP_IMAGE) $(COMPOSE) -f docker-compose.yml -f docker-compose.scale.yml up -d --wait --scale app=2

smoke:
	python3 scripts/smoke.py --base-url http://127.0.0.1:8080

test:
	docker build $(DOCKER_BUILD_FLAGS) --target test -t $(IMAGE):test .
	docker run --rm $(IMAGE):test sh -c 'go test $$(go list ./... | sed "\\|/internal/e2e$$|d")'

# Docker socket lets Testcontainers create a temporary PostgreSQL.
test-e2e:
	docker build $(DOCKER_BUILD_FLAGS) --target test -t $(IMAGE):test .
	docker run --rm --network host -v /var/run/docker.sock:/var/run/docker.sock -e TESTCONTAINERS_HOST_OVERRIDE=127.0.0.1 $(IMAGE):test go test ./internal/e2e/... -v -count=1 -timeout 3m

tools:
	GOBIN=$(GOBIN) go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)
	GOBIN=$(GOBIN) go install github.com/sqlc-dev/sqlc/cmd/sqlc@$(SQLC_VERSION)

generate: tools
	mkdir -p internal/api internal/db/sqlc
	$(GOBIN)/oapi-codegen -generate chi-server,types,strict-server,spec -package api -o internal/api/api.gen.go api/openapi.yaml
	$(GOBIN)/sqlc generate

local-build: generate
	CGO_ENABLED=0 go build -trimpath -o bin/server ./cmd/server
	CGO_ENABLED=0 go build -trimpath -o bin/migrate ./cmd/migrate

archive:
	mkdir -p dist
	git archive --format=zip --prefix=tbank-sre/ -o dist/tbank-sre.zip HEAD
