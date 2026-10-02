FROM golang:1.26.1-alpine3.23 AS tools
WORKDIR /src
ENV CGO_ENABLED=0
ARG OAPI_CODEGEN_VERSION=v2.8.0
ARG SQLC_VERSION=v1.31.1
RUN go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@${OAPI_CODEGEN_VERSION} \
    && go install github.com/sqlc-dev/sqlc/cmd/sqlc@${SQLC_VERSION}
COPY go.mod go.sum ./
RUN go mod download

FROM tools AS source
COPY . .
RUN mkdir -p internal/api internal/db/sqlc \
    && oapi-codegen -generate chi-server,types,strict-server,spec -package api -o internal/api/api.gen.go api/openapi.yaml \
    && sqlc generate

# Same source, generated code and toolchain as the release.
FROM source AS test
CMD ["go", "test", "./..."]

FROM source AS builder
RUN go build -trimpath -ldflags='-s -w' -o /out/server ./cmd/server \
    && go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate

FROM alpine:3.23.3 AS runtime
ARG VERSION=local
ARG REVISION=unknown
LABEL org.opencontainers.image.title="T-Bank SRE Marketplace" \
      org.opencontainers.image.version=${VERSION} \
      org.opencontainers.image.revision=${REVISION}
WORKDIR /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /out/server /out/migrate /app/
USER 65532:65532
EXPOSE 8080
CMD ["/app/server"]
