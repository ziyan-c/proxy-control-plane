package security

import (
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

type tokenPayload struct {
	Subject   string `json:"sub"`
	ExpiresAt int64  `json:"exp"`
}

func CreateAccessToken(subject string, secretKey string, ttl time.Duration) (string, error) {
	header := map[string]string{"alg": "HS256", "typ": "JWT"}
	payload := tokenPayload{
		Subject:   subject,
		ExpiresAt: time.Now().UTC().Add(ttl).Unix(),
	}
	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." + base64.RawURLEncoding.EncodeToString(payloadJSON)
	signature := sign([]byte(signingInput), []byte(secretKey))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func VerifyAccessToken(token string, secretKey string) (string, bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", false
	}
	signingInput := parts[0] + "." + parts[1]
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", false
	}
	if !hmac.Equal(signature, sign([]byte(signingInput), []byte(secretKey))) {
		return "", false
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var payload tokenPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return "", false
	}
	if payload.Subject == "" || payload.ExpiresAt < time.Now().UTC().Unix() {
		return "", false
	}
	return payload.Subject, true
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
