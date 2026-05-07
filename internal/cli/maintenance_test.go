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
