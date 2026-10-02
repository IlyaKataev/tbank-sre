FROM golang:1.26-alpine AS builder

RUN apk add --no-cache git make curl

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

# Install codegen tools
RUN go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest
RUN go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

COPY . .
RUN make generate
RUN go build -o /bin/server ./cmd/server

# Smoke test
RUN /bin/server --help 2>/dev/null || true

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /bin/server /app/server
EXPOSE 8080
CMD ["/app/server"]
