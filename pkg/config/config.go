package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	GRPCPort    string
	MetricsPort string

	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisPoolSize int

	// Default bucket settings (can be overridden per-request)
	DefaultBurst int64
	DefaultRate  float64

	// gRPC settings
	MaxRecvMsgSize int
	MaxConcurrent  int

	// Redis timeouts
	RedisDialTimeout  time.Duration
	RedisReadTimeout  time.Duration
	RedisWriteTimeout time.Duration
}

func Load() *Config {
	return &Config{
		GRPCPort:          envOrDefault("GRPC_PORT", "50051"),
		MetricsPort:       envOrDefault("METRICS_PORT", "9090"),
		RedisAddr:         envOrDefault("REDIS_ADDR", "localhost:6379"),
		RedisPassword:     envOrDefault("REDIS_PASSWORD", ""),
		RedisDB:           envOrDefaultInt("REDIS_DB", 0),
		RedisPoolSize:     envOrDefaultInt("REDIS_POOL_SIZE", 100),
		DefaultBurst:      int64(envOrDefaultInt("DEFAULT_BURST", 100)),
		DefaultRate:       envOrDefaultFloat("DEFAULT_RATE", 10.0),
		MaxRecvMsgSize:    4 * 1024 * 1024, // 4MB
		MaxConcurrent:     envOrDefaultInt("MAX_CONCURRENT_STREAMS", 1000),
		RedisDialTimeout:  time.Duration(envOrDefaultInt("REDIS_DIAL_TIMEOUT_MS", 500)) * time.Millisecond,
		RedisReadTimeout:  time.Duration(envOrDefaultInt("REDIS_READ_TIMEOUT_MS", 200)) * time.Millisecond,
		RedisWriteTimeout: time.Duration(envOrDefaultInt("REDIS_WRITE_TIMEOUT_MS", 200)) * time.Millisecond,
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envOrDefaultInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func envOrDefaultFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}