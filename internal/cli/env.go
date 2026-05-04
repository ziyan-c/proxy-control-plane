package cli

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func defaultDBProfile(configDir string) string {
	values, _ := readEnvMap(filepath.Join(configDir, "cli.env"))
	if value := normalizeDBProfile(values["DB"]); value != "" {
		return value
	}
	if value := normalizeDBProfile(values["PCP_DB_PROFILE"]); value != "" {
		return value
	}
	return "local"
}

func resolveDBProfile(cmdDB string, configDir string) (string, error) {
	dbProfile := cmdDB
	if strings.TrimSpace(dbProfile) == "" {
		dbProfile = defaultDBProfile(configDir)
	}
	dbProfile = normalizeDBProfile(dbProfile)
	if dbProfile == "" {
		return "", errors.New(`--db must be "local" or "remote"`)
	}
	return dbProfile, nil
}

func normalizeDBProfile(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "local", "remote":
		return value
	default:
		return ""
	}
}

func apiEnvFile(configDir string, dbProfile string) (string, error) {
	switch dbProfile {
	case "local":
		return filepath.Join(configDir, "api.local.env"), nil
	case "remote":
		return filepath.Join(configDir, "api.remote.env"), nil
	default:
		return "", errors.New(`--db must be "local" or "remote"`)
	}
}

func dockerAPIEnvFile(configDir string, dbProfile string) (string, error) {
	switch dbProfile {
	case "local":
		return filepath.Join(configDir, "api.docker.env"), nil
	case "remote":
		return filepath.Join(configDir, "api.remote.env"), nil
	default:
		return "", errors.New(`--db must be "local" or "remote"`)
	}
}

func loadEnvFile(path string, overwrite bool) error {
	values, err := readEnvMap(path)
	if err != nil {
		return err
	}
	for key, value := range values {
		if !overwrite {
			if _, exists := os.LookupEnv(key); exists {
				continue
			}
		}
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

func readEnvMap(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		values[key] = cleanEnvValue(value)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return values, nil
}

func cleanEnvValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 {
		return value
	}
	if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
	}
	if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "'"), "'")
	}
	return value
}
