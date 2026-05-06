package cli

import (
	"testing"
	"time"
)

func TestParseRetentionDuration(t *testing.T) {
	duration, err := parseRetentionDuration("90d")
	if err != nil {
		t.Fatalf("parseRetentionDuration() error = %v", err)
	}
	if duration != 90*24*time.Hour {
		t.Fatalf("duration = %v, want 2160h", duration)
	}

	duration, err = parseRetentionDuration("720h")
	if err != nil {
		t.Fatalf("parseRetentionDuration() error = %v", err)
	}
	if duration != 720*time.Hour {
		t.Fatalf("duration = %v, want 720h", duration)
	}

	if _, err := parseRetentionDuration("0d"); err == nil {
		t.Fatal("parseRetentionDuration(0d) succeeded, want error")
	}
}

func TestParseSizeBytes(t *testing.T) {
	tests := []struct {
		value string
		want  int64
	}{
		{value: "0", want: 0},
		{value: "512", want: 512},
		{value: "2GB", want: 2 * 1024 * 1024 * 1024},
		{value: "128MB", want: 128 * 1024 * 1024},
		{value: "1GiB", want: 1024 * 1024 * 1024},
	}

	for _, tc := range tests {
		t.Run(tc.value, func(t *testing.T) {
			got, err := parseSizeBytes(tc.value)
			if err != nil {
				t.Fatalf("parseSizeBytes() error = %v", err)
			}
			if got != tc.want {
				t.Fatalf("parseSizeBytes() = %d, want %d", got, tc.want)
			}
		})
	}

	if _, err := parseSizeBytes("-1GB"); err == nil {
		t.Fatal("parseSizeBytes(-1GB) succeeded, want error")
	}
}
