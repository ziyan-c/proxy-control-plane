package security

import (
	"strings"
	"testing"
	"time"
)

func TestAccessTokenRoundTrip(t *testing.T) {
	token, err := CreateAccessToken(AccessClaims{
		Subject: PrincipalSubjectConfiguredAdmin,
		Role:    PrincipalTypeAdmin,
	}, "test-secret-key", time.Minute)
	if err != nil {
		t.Fatalf("CreateAccessToken() error = %v", err)
	}
	claims, ok := VerifyAccessToken(token, "test-secret-key")
	if !ok {
		t.Fatal("VerifyAccessToken() rejected valid token")
	}
	if claims.Subject != PrincipalSubjectConfiguredAdmin {
		t.Fatalf("subject = %q", claims.Subject)
	}
	if claims.Role != PrincipalTypeAdmin {
		t.Fatalf("role = %q", claims.Role)
	}
	if _, ok := VerifyAccessToken(token, "wrong-secret"); ok {
		t.Fatal("VerifyAccessToken() accepted wrong secret")
	}
}

func TestAdminAccessTokenRequiresConfiguredSubject(t *testing.T) {
	if _, err := CreateAccessToken(AccessClaims{
		Subject: "admin@example.com",
		Role:    PrincipalTypeAdmin,
	}, "test-secret-key", time.Minute); err == nil {
		t.Fatal("CreateAccessToken accepted admin token with email subject")
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

func TestAuthSessionVersionChangesWithInputs(t *testing.T) {
	version := AuthSessionVersion("secret-key", "admin", "admin@example.com", "password", "epoch-1")
	if version == "" {
		t.Fatal("AuthSessionVersion returned empty version")
	}
	if version != AuthSessionVersion("secret-key", "admin", "admin@example.com", "password", "epoch-1") {
		t.Fatal("AuthSessionVersion is not stable")
	}
	if version == AuthSessionVersion("secret-key", "admin", "admin@example.com", "password", "epoch-2") {
		t.Fatal("AuthSessionVersion did not change when epoch changed")
	}
	if version == AuthSessionVersion("other-secret", "admin", "admin@example.com", "password", "epoch-1") {
		t.Fatal("AuthSessionVersion did not change when secret changed")
	}
}

func TestEncryptStringWithBase64KeyRoundTrip(t *testing.T) {
	key := "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY="
	encrypted, err := EncryptStringWithBase64Key(key, "secret-token")
	if err != nil {
		t.Fatalf("EncryptStringWithBase64Key() error = %v", err)
	}
	if encrypted == "" || encrypted == "secret-token" {
		t.Fatalf("encrypted value = %q, want non-empty ciphertext", encrypted)
	}
	decrypted, err := DecryptStringWithBase64Key(key, encrypted)
	if err != nil {
		t.Fatalf("DecryptStringWithBase64Key() error = %v", err)
	}
	if decrypted != "secret-token" {
		t.Fatalf("decrypted = %q, want secret-token", decrypted)
	}
}

func TestParseDatabaseEncryptionKeyRejectsInvalidLength(t *testing.T) {
	if _, err := ParseDatabaseEncryptionKey("c2hvcnQ="); err == nil {
		t.Fatal("ParseDatabaseEncryptionKey accepted short key")
	}
}
