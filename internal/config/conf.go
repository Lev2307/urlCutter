package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port              string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	ShutdownTimeout   time.Duration
	LogLevel          slog.Level
	DBName            string
	HostNameURL       string
	RateLimitRPS      float64
	RateLimitBurst    float64
	RateLimitTTL      time.Duration
}

func getString(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getFloat(key string, def float64) (float64, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	fl, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return fl, nil
}

func getDuration(key string, def time.Duration) (time.Duration, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}

func Load() (Config, error) {
	readTm, err := getDuration("READ_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	writeTm, err := getDuration("WRITE_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	idleTm, err := getDuration("IDLE_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	readHeaderTm, err := getDuration("READ_HEADER_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	shutDownTm, err := getDuration("SHUTDOWN_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}

	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(getString("LOG_LEVEL", "INFO"))); err != nil {
		return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
	}

	rlRPS, err := getFloat("RATE_LIMIT_RPS", 10)
	if err != nil {
		return Config{}, err
	}
	rlBurst, err := getFloat("RATE_LIMIT_BURST", 20)
	if err != nil {
		return Config{}, err
	}
	rlTTL, err := getDuration("RATE_LIMIT_TTL", 60*time.Second)
	if err != nil {
		return Config{}, err
	}
	return Config{
		Port:              getString("PORT", "8080"),
		ReadTimeout:       readTm,
		WriteTimeout:      writeTm,
		IdleTimeout:       idleTm,
		ReadHeaderTimeout: readHeaderTm,
		ShutdownTimeout:   shutDownTm,
		LogLevel:          lvl,
		DBName:            getString("DB_NAME", "default.db"),
		HostNameURL:       getString("HOSTNAME_URL", "localhost"),
		RateLimitRPS:      rlRPS,
		RateLimitBurst:    rlBurst,
		RateLimitTTL:      rlTTL,
	}, nil
}
