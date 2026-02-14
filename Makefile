.PHONY: proto build test run docker-up docker-down loadtest lint clean

# ── Protobuf ──────────────────────────────────────────────
proto:
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/ratelimit.proto

# ── Build ─────────────────────────────────────────────────
build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/ratelimiter ./cmd/server

# ── Test ──────────────────────────────────────────────────
test:
	go test -v -race -count=1 ./pkg/...

test-bench:
	go test -bench=. -benchmem ./pkg/limiter/...

# ── Run locally ───────────────────────────────────────────
run: build
	REDIS_ADDR=localhost:6379 ./bin/ratelimiter

# ── Docker ────────────────────────────────────────────────
docker-up:
	docker compose -f deployments/docker-compose.yml up --build -d

docker-down:
	docker compose -f deployments/docker-compose.yml down -v

docker-logs:
	docker compose -f deployments/docker-compose.yml logs -f

# ── Load test ─────────────────────────────────────────────
loadtest:
	go run scripts/loadtest.go \
		-addr localhost:50051 \
		-rps 5000 \
		-duration 30s \
		-keys 100 \
		-conns 10 \
		-workers 50

loadtest-heavy:
	go run scripts/loadtest.go \
		-addr localhost:50051 \
		-rps 10000 \
		-duration 60s \
		-keys 500 \
		-conns 20 \
		-workers 100

# ── Lint ──────────────────────────────────────────────────
lint:
	golangci-lint run ./...

# ── Clean ─────────────────────────────────────────────────
clean:
	rm -rf bin/
	docker compose -f deployments/docker-compose.yml down -v