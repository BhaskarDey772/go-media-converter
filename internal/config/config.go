package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port           string
	WorkerCount    int
	MaxUploadBytes int64
	OutputDir      string
	UploadDir      string
}

func Load() Config {
	return Config{
		Port:           envOr("PORT", "8080"),
		WorkerCount:    envInt("WORKER_COUNT", 2),
		MaxUploadBytes: envInt64("MAX_UPLOAD_BYTES", 524288000),
		OutputDir:      envOr("OUTPUT_DIR", "./output"),
		UploadDir:      envOr("UPLOAD_DIR", "./uploads"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
