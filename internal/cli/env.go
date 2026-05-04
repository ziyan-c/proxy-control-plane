package cli

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func appEnvFile(configDir string) string {
	return filepath.Join(configDir, "app.env")
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
