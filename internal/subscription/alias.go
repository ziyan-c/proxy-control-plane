package subscription

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/ziyan-c/proxy-control-plane/internal/security"
)

func CanonicalAliasPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("subscription alias path is required")
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("parse subscription alias URL: %w", err)
		}
		value = parsed.Path
	}
	if i := strings.IndexAny(value, "?#"); i >= 0 {
		value = value[:i]
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("subscription alias path is required")
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if value == "/" {
		return "", fmt.Errorf("subscription alias path cannot be /")
	}
	if strings.HasPrefix(value, "/legacy-sub/") || value == "/legacy-sub" {
		return "", fmt.Errorf("subscription alias path cannot use the /legacy-sub prefix")
	}
	return value, nil
}

func AliasDigest(path string) (string, error) {
	path, err := CanonicalAliasPath(path)
	if err != nil {
		return "", err
	}
	return security.TokenDigest(path), nil
}
