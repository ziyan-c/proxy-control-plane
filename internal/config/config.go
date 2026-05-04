package config

import (
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
		AutoMigrate:              getEnvBool("PCP_AUTO_MIGRATE", true),
	}
}

func (c Config) AccessTokenTTL() time.Duration {
	return time.Duration(c.AccessTokenExpireMinutes) * time.Minute
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

func normalizeDatabaseURL(value string) string {
	return strings.Replace(value, "postgresql+psycopg://", "postgres://", 1)
}
