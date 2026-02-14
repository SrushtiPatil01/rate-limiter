# ── Build stage ────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git protobuf protobuf-dev

WORKDIR /app

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Generate protobuf (if not committed)
# RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
#     go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && \
#     protoc --go_out=. --go-grpc_out=. proto/ratelimit.proto

# Build statically linked binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /ratelimiter ./cmd/server

# ── Runtime stage ─────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 1000 appuser

COPY --from=builder /ratelimiter /usr/local/bin/ratelimiter

USER appuser

EXPOSE 50051 9090

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost:9090/health || exit 1

ENTRYPOINT ["ratelimiter"]