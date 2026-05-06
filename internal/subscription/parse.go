package subscription

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

type VLESSLink struct {
	Raw              string
	UUID             string
	EmailTag         string
	Flow             string
	Host             string
	Port             int
	Transport        string
	Security         string
	SNI              string
	Fingerprint      string
	ALPN             string
	Path             string
	HostHeader       string
	RealityPublicKey string
	RealityShortID   string
}

type ParseResult struct {
	VLESSLinks      []VLESSLink
	UnsupportedURIs int
}

func ParseVLESSLinks(data []byte) ([]VLESSLink, error) {
	result, err := ParseLinks(data)
	if err != nil {
		return nil, err
	}
	return result.VLESSLinks, nil
}

func ParseLinks(data []byte) (ParseResult, error) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return ParseResult{}, nil
	}

	if !strings.Contains(strings.ToLower(text), "vless://") {
		decoded, ok := decodeSubscriptionBase64(text)
		if ok {
			text = decoded
		}
	}

	lines := strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r'
	})
	links := make([]VLESSLink, 0, len(lines))
	unsupported := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(line), "vless://") {
			if looksLikeURI(line) {
				unsupported++
			}
			continue
		}
		link, err := parseVLESSLink(line)
		if err != nil {
			return ParseResult{}, err
		}
		links = append(links, link)
	}
	return ParseResult{VLESSLinks: links, UnsupportedURIs: unsupported}, nil
}

func decodeSubscriptionBase64(text string) (string, bool) {
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\n', '\r', '\t':
			return -1
		default:
			return r
		}
	}, text)
	if compact == "" {
		return "", false
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(compact)
		if err != nil {
			continue
		}
		decodedText := strings.TrimSpace(string(decoded))
		if strings.Contains(strings.ToLower(decodedText), "vless://") {
			return decodedText, true
		}
	}
	return "", false
}

func parseVLESSLink(raw string) (VLESSLink, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return VLESSLink{}, fmt.Errorf("parse VLESS URI: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "vless") {
		return VLESSLink{}, fmt.Errorf("unsupported subscription URI scheme %q", parsed.Scheme)
	}
	uuid := strings.TrimSpace(parsed.User.Username())
	if uuid == "" {
		return VLESSLink{}, fmt.Errorf("VLESS URI is missing UUID")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return VLESSLink{}, fmt.Errorf("VLESS URI is missing host")
	}
	port := 443
	if portText := parsed.Port(); portText != "" {
		value, err := strconv.Atoi(portText)
		if err != nil || value < 1 || value > 65535 {
			return VLESSLink{}, fmt.Errorf("VLESS URI has invalid port %q", portText)
		}
		port = value
	}

	query := parsed.Query()
	return VLESSLink{
		Raw:              raw,
		UUID:             uuid,
		EmailTag:         strings.TrimSpace(parsed.Fragment),
		Flow:             strings.TrimSpace(query.Get("flow")),
		Host:             host,
		Port:             port,
		Transport:        strings.TrimSpace(query.Get("type")),
		Security:         strings.TrimSpace(query.Get("security")),
		SNI:              strings.TrimSpace(query.Get("sni")),
		Fingerprint:      strings.TrimSpace(query.Get("fp")),
		ALPN:             strings.TrimSpace(query.Get("alpn")),
		Path:             strings.TrimSpace(query.Get("path")),
		HostHeader:       strings.TrimSpace(query.Get("host")),
		RealityPublicKey: strings.TrimSpace(query.Get("pbk")),
		RealityShortID:   strings.TrimSpace(query.Get("sid")),
	}, nil
}

func looksLikeURI(line string) bool {
	i := strings.Index(line, "://")
	if i <= 0 {
		return false
	}
	scheme := line[:i]
	for _, r := range scheme {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '+' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}
