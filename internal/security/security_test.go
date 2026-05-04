package security

import (
	"strings"
	"testing"
	"time"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	token, err := CreateAccessToken("admin@example.com", "test-secret-key", time.Minute)
	if err != nil {
		t.Fatalf("CreateAccessToken() error = %v", err)
	}
	subject, ok := VerifyAccessToken(token, "test-secret-key")
	if !ok {
		t.Fatal("VerifyAccessToken() rejected valid token")
	}
	if subject != "admin@example.com" {
		t.Fatalf("subject = %q", subject)
	}
	if _, ok := VerifyAccessToken(token, "wrong-secret"); ok {
		t.Fatal("VerifyAccessToken() accepted wrong secret")
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := PasswordHash("change-me-admin-password")
	if err != nil {
		t.Fatalf("PasswordHash() error = %v", err)
	}
	if !strings.HasPrefix(hash, "pbkdf2_sha256$") {
		t.Fatalf("hash has unexpected format: %s", hash)
	}
	if !VerifyPassword("change-me-admin-password", hash) {
		t.Fatal("VerifyPassword() rejected valid password")
	}
	if VerifyPassword("wrong-password", hash) {
		t.Fatal("VerifyPassword() accepted invalid password")
	}
}

func TestTokenDigest(t *testing.T) {
	digest := TokenDigest("token")
	if len(digest) != 64 {
		t.Fatalf("digest length = %d", len(digest))
	}
	if digest != TokenDigest("token") {
		t.Fatal("digest is not stable")
	}
}
