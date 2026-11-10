package auth

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode"
)

const (
	passwordIterations = 600_000
	passwordSaltBytes  = 16
	passwordKeyBytes   = 32
)

// HashPassword creates a salted PBKDF2-HMAC-SHA256 verifier for a local user.
func HashPassword(password string) (string, error) {
	if len(password) == 0 || len(password) > 1024 {
		return "", errors.New("password length must be between 1 and 1024 bytes")
	}
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, passwordKeyBytes)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256:%d:%s:%s", passwordIterations,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

type passwordVerifier struct {
	salt []byte
	key  []byte
}

func parsePasswordVerifier(encoded string) (passwordVerifier, error) {
	parts := strings.Split(encoded, ":")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != strconv.Itoa(passwordIterations) {
		return passwordVerifier{}, errors.New("invalid password hash format")
	}
	salt, saltErr := base64.RawStdEncoding.DecodeString(parts[2])
	key, keyErr := base64.RawStdEncoding.DecodeString(parts[3])
	if saltErr != nil || keyErr != nil || len(salt) != passwordSaltBytes || len(key) != passwordKeyBytes {
		return passwordVerifier{}, errors.New("invalid password hash data")
	}
	return passwordVerifier{salt: salt, key: key}, nil
}

func (verifier passwordVerifier) matches(password string) bool {
	if len(password) == 0 || len(password) > 1024 {
		return false
	}
	key, err := pbkdf2.Key(sha256.New, password, verifier.salt, passwordIterations, passwordKeyBytes)
	return err == nil && subtle.ConstantTimeCompare(key, verifier.key) == 1
}

// PasswordStore authenticates local HTTP Basic credentials and knows registered users.
type PasswordStore struct {
	users map[string]passwordVerifier
	dummy passwordVerifier
}

func NewPasswordStore(hashes map[string]string) (*PasswordStore, error) {
	if len(hashes) == 0 {
		return nil, errors.New("at least one user is required")
	}
	store := &PasswordStore{users: make(map[string]passwordVerifier, len(hashes))}
	for id, encoded := range hashes {
		if !validUserID(id) {
			return nil, errors.New("invalid user ID")
		}
		verifier, err := parsePasswordVerifier(encoded)
		if err != nil {
			return nil, fmt.Errorf("user %q: %w", id, err)
		}
		store.users[id] = verifier
		store.dummy = verifier
	}
	return store, nil
}

func (store *PasswordStore) Exists(id string) bool {
	if store == nil {
		return false
	}
	_, ok := store.users[id]
	return ok
}

func (store *PasswordStore) Authenticate(request *http.Request) (string, bool) {
	if store == nil || request == nil || len(request.Header.Values("Authorization")) != 1 {
		return "", false
	}
	id, password, ok := request.BasicAuth()
	if !ok {
		return "", false
	}
	verifier, exists := store.users[id]
	if !exists {
		verifier = store.dummy
	}
	if !verifier.matches(password) || !exists {
		return "", false
	}
	return id, true
}

func validUserID(id string) bool {
	if id == "" || len(id) > 128 || strings.TrimSpace(id) != id {
		return false
	}
	for _, r := range id {
		if r == ':' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
