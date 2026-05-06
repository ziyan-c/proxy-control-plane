package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppName                  string
	Environment              string
	ListenAddr               string
	DatabaseURL              string
	AdminEmail               string
	AdminPassword            string
	SecretKey                string
	AccessTokenExpireMinutes int
	AutoCreateDatabase       bool
	AutoMigrate              bool
	RuntimeSyncEnabled       bool
	RuntimeSyncInterval      time.Duration
	RuntimeSyncTimeout       time.Duration
	RuntimeSyncConcurrency   int
	TrafficSyncEnabled       bool
	TrafficSyncInterval      time.Duration
	TrafficSyncTimeout       time.Duration
	TrafficSyncConcurrency   int
}

func Load() Config {
	return Config{
		AppName:                  getEnv("PCP_APP_NAME", "proxy-control-plane"),
		Environment:              getEnv("PCP_ENVIRONMENT", "local"),
		ListenAddr:               getEnv("PCP_LISTEN_ADDR", ":9710"),
		DatabaseURL:              normalizeDatabaseURL(getEnv("PCP_DATABASE_URL", "postgres://proxy_control_app:change-me@127.0.0.1:5432/proxy_control?sslmode=disable")),
		AdminEmail:               getEnv("PCP_ADMIN_EMAIL", "admin@example.com"),
		AdminPassword:            getEnv("PCP_ADMIN_PASSWORD", "change-me-admin-password"),
		SecretKey:                getEnv("PCP_SECRET_KEY", "change-me-with-openssl-rand-base64-32"),
		AccessTokenExpireMinutes: getEnvInt("PCP_ACCESS_TOKEN_EXPIRE_MINUTES", 60),
		AutoCreateDatabase:       getEnvBool("PCP_AUTO_CREATE_DATABASE", true),
		AutoMigrate:              getEnvBool("PCP_AUTO_MIGRATE", false),
		RuntimeSyncEnabled:       getEnvBool("PCP_RUNTIME_SYNC_ENABLED", false),
		RuntimeSyncInterval:      getEnvDuration("PCP_RUNTIME_SYNC_INTERVAL", 5*time.Minute),
		RuntimeSyncTimeout:       getEnvDuration("PCP_RUNTIME_SYNC_TIMEOUT", 30*time.Second),
		RuntimeSyncConcurrency:   getEnvInt("PCP_RUNTIME_SYNC_CONCURRENCY", 3),
		TrafficSyncEnabled:       getEnvBool("PCP_TRAFFIC_SYNC_ENABLED", false),
		TrafficSyncInterval:      getEnvDuration("PCP_TRAFFIC_SYNC_INTERVAL", 10*time.Minute),
		TrafficSyncTimeout:       getEnvDuration("PCP_TRAFFIC_SYNC_TIMEOUT", 30*time.Second),
		TrafficSyncConcurrency:   getEnvInt("PCP_TRAFFIC_SYNC_CONCURRENCY", 3),
	}
}

func (c Config) AccessTokenTTL() time.Duration {
	return time.Duration(c.AccessTokenExpireMinutes) * time.Minute
}

func (c Config) ValidateServer() error {
	var problems []string
	if strings.EqualFold(strings.TrimSpace(c.AdminEmail), "admin@example.com") {
		problems = append(problems, "PCP_ADMIN_EMAIL must not use the example value")
	}
	if isPlaceholderSecret(c.AdminPassword) || len(c.AdminPassword) < 12 {
		problems = append(problems, "PCP_ADMIN_PASSWORD must be changed and contain at least 12 characters")
	}
	if isPlaceholderSecret(c.SecretKey) || len(c.SecretKey) < 32 {
		problems = append(problems, "PCP_SECRET_KEY must be changed and contain at least 32 characters")
	}
	if c.AccessTokenExpireMinutes <= 0 {
		problems = append(problems, "PCP_ACCESS_TOKEN_EXPIRE_MINUTES must be greater than 0")
	}
	if c.RuntimeSyncEnabled {
		if c.RuntimeSyncInterval <= 0 {
			problems = append(problems, "PCP_RUNTIME_SYNC_INTERVAL must be greater than 0")
		}
		if c.RuntimeSyncTimeout <= 0 {
			problems = append(problems, "PCP_RUNTIME_SYNC_TIMEOUT must be greater than 0")
		}
		if c.RuntimeSyncConcurrency <= 0 {
			problems = append(problems, "PCP_RUNTIME_SYNC_CONCURRENCY must be greater than 0")
		}
	}
	if c.TrafficSyncEnabled {
		if c.TrafficSyncInterval <= 0 {
			problems = append(problems, "PCP_TRAFFIC_SYNC_INTERVAL must be greater than 0")
		}
		if c.TrafficSyncTimeout <= 0 {
			problems = append(problems, "PCP_TRAFFIC_SYNC_TIMEOUT must be greater than 0")
		}
		if c.TrafficSyncConcurrency <= 0 {
			problems = append(problems, "PCP_TRAFFIC_SYNC_CONCURRENCY must be greater than 0")
		}
	}
	if len(problems) > 0 {
		return errors.New("invalid server configuration: " + strings.Join(problems, "; "))
	}
	return nil
}

func getEnv(key string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func getEnvBool(key string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	if value == "" {
		return fallback
	}
	switch value {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func normalizeDatabaseURL(value string) string {
	return strings.Replace(value, "postgresql+psycopg://", "postgres://", 1)
}

func isPlaceholderSecret(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	if strings.HasPrefix(value, "change-me") || strings.HasPrefix(value, "change-this") {
		return true
	}
	return strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">")
}
