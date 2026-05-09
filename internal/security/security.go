package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"strings"
	"time"
)

const defaultPBKDF2Iterations = 260_000
const databaseEncryptionKeyBytes = 32
const encryptedStringVersion = "v1"
const PrincipalTypeAdmin = "admin"
const PrincipalTypeCustomer = "customer"
const PrincipalSubjectConfiguredAdmin = "configured-admin"

func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(b[0:4]),
		binary.BigEndian.Uint16(b[4:6]),
		binary.BigEndian.Uint16(b[6:8]),
		binary.BigEndian.Uint16(b[8:10]),
		b[10:16],
	), nil
}

func NewRandomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func TokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func AuthSessionVersion(secretKey string, parts ...string) string {
	mac := hmac.New(sha256.New, []byte(secretKey))
	for _, part := range parts {
		mac.Write([]byte{0})
		mac.Write([]byte(part))
	}
	return hex.EncodeToString(mac.Sum(nil))
}

func ParseDatabaseEncryptionKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("database encryption key must be base64: %w", err)
	}
	if len(key) != databaseEncryptionKeyBytes {
		return nil, fmt.Errorf("database encryption key must decode to %d bytes", databaseEncryptionKeyBytes)
	}
	return key, nil
}

func EncryptStringWithBase64Key(encodedKey string, plaintext string) (string, error) {
	key, err := ParseDatabaseEncryptionKey(encodedKey)
	if err != nil {
		return "", err
	}
	if len(key) == 0 {
		return "", nil
	}
	return EncryptString(key, plaintext)
}

func DecryptStringWithBase64Key(encodedKey string, encrypted string) (string, error) {
	key, err := ParseDatabaseEncryptionKey(encodedKey)
	if err != nil {
		return "", err
	}
	if len(key) == 0 {
		return "", fmt.Errorf("database encryption key is required")
	}
	return DecryptString(key, encrypted)
}

func EncryptString(key []byte, plaintext string) (string, error) {
	if len(key) != databaseEncryptionKeyBytes {
		return "", fmt.Errorf("database encryption key must be %d bytes", databaseEncryptionKeyBytes)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nil, nonce, []byte(plaintext), nil)
	return encryptedStringVersion + ":" +
		base64.RawURLEncoding.EncodeToString(nonce) + ":" +
		base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

func DecryptString(key []byte, encrypted string) (string, error) {
	if len(key) != databaseEncryptionKeyBytes {
		return "", fmt.Errorf("database encryption key must be %d bytes", databaseEncryptionKeyBytes)
	}
	parts := strings.Split(encrypted, ":")
	if len(parts) != 3 || parts[0] != encryptedStringVersion {
		return "", fmt.Errorf("unsupported encrypted string format")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode encrypted string nonce: %w", err)
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("decode encrypted string ciphertext: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(nonce) != aead.NonceSize() {
		return "", fmt.Errorf("encrypted string nonce has invalid length")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func PasswordHash(password string) (string, error) {
	salt, err := NewRandomToken()
	if err != nil {
		return "", err
	}
	digest := pbkdf2Key([]byte(password), []byte(salt), defaultPBKDF2Iterations, 32, sha256.New)
	return fmt.Sprintf("pbkdf2_sha256$%d$%s$%s", defaultPBKDF2Iterations, salt, hex.EncodeToString(digest)), nil
}

func VerifyPassword(password string, stored string) bool {
	if !strings.HasPrefix(stored, "pbkdf2_sha256$") {
		return hmac.Equal([]byte(password), []byte(stored))
	}

	parts := strings.Split(stored, "$")
	if len(parts) != 4 {
		return false
	}
	var iterations int
	if _, err := fmt.Sscanf(parts[1], "%d", &iterations); err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	digest := pbkdf2Key([]byte(password), []byte(parts[2]), iterations, len(expected), sha256.New)
	return hmac.Equal(digest, expected)
}

type AccessClaims struct {
	Subject   string `json:"sub"`
	Role      string `json:"role"`
	IssuedAt  int64  `json:"iat,omitempty"`
	ExpiresAt int64  `json:"exp"`
}

func (c AccessClaims) Actor() string {
	if c.Role != "" && c.Subject != "" {
		return c.Role + ":" + c.Subject
	}
	return c.Subject
}

func CreateAccessToken(claims AccessClaims, secretKey string, ttl time.Duration) (string, error) {
	if claims.Subject == "" || claims.Role == "" {
		return "", errors.New("access token subject and role are required")
	}
	if claims.Role != PrincipalTypeAdmin && claims.Role != PrincipalTypeCustomer {
		return "", errors.New("unsupported access token role")
	}
	if claims.Role == PrincipalTypeAdmin && claims.Subject != PrincipalSubjectConfiguredAdmin {
		return "", errors.New("admin access token requires configured admin subject")
	}
	now := time.Now().UTC()
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	claims.IssuedAt = now.Unix()
	claims.ExpiresAt = now.Add(ttl).Unix()
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	signature := sign([]byte(signingInput), []byte(secretKey))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func VerifyAccessToken(token string, secretKey string) (AccessClaims, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessClaims{}, false
	}
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return AccessClaims{}, false
	}
	if !hmac.Equal(signature, sign([]byte(signingInput), []byte(secretKey))) {
		return AccessClaims{}, false
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return AccessClaims{}, false
	}
	var claims AccessClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return AccessClaims{}, false
	}
	if claims.Subject == "" || claims.Role == "" || claims.ExpiresAt < time.Now().UTC().Unix() {
		return AccessClaims{}, false
	}
	if claims.Role != PrincipalTypeAdmin && claims.Role != PrincipalTypeCustomer {
		return AccessClaims{}, false
	}
	if claims.Role == PrincipalTypeAdmin && claims.Subject != PrincipalSubjectConfiguredAdmin {
		return AccessClaims{}, false
	}
	return claims, true
}

func sign(data []byte, secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(data)
	return mac.Sum(nil)
}

func pbkdf2Key(password, salt []byte, iter, keyLen int, h func() hash.Hash) []byte {
	if iter <= 0 || keyLen <= 0 {
		panic(errors.New("invalid pbkdf2 parameters"))
	}
	prf := hmac.New(h, password)
	hashLen := prf.Size()
	numBlocks := (keyLen + hashLen - 1) / hashLen
	var out []byte
	var blockBuf [4]byte
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(blockBuf[:], uint32(block))
		prf.Write(blockBuf[:])
		u := prf.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iter; i++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for x := range t {
				t[x] ^= u[x]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}
