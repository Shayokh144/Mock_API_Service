package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// TokenFile is the bearer token stored next to the rules file.
type TokenFile struct {
	mu    sync.RWMutex
	path  string
	token string
}

// LoadToken reads the token file or creates one. The directory is 0700 and the file is 0600.
// The token itself is not returned to logs.
func LoadToken(path string) (*TokenFile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("admin token path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		token, err = newToken()
		if err != nil {
			return nil, err
		}
		if err := writeToken(path, token); err != nil {
			return nil, err
		}
	} else if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}
	return &TokenFile{path: path, token: token}, nil
}

// Path is the token file path. It does not include the token.
func (t *TokenFile) Path() string {
	return t.path
}

// Match reports whether the presented bearer token is the current one.
func (t *TokenFile) Match(presented string) bool {
	t.mu.RLock()
	want := t.token
	t.mu.RUnlock()
	sumGot := sha256.Sum256([]byte(presented))
	sumWant := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(sumGot[:], sumWant[:]) == 1
}

// Rotate replaces the token on disk. The previous token stops matching.
func (t *TokenFile) Rotate() error {
	next, err := newToken()
	if err != nil {
		return err
	}
	if err := writeToken(t.path, next); err != nil {
		return err
	}
	t.mu.Lock()
	t.token = next
	t.mu.Unlock()
	return nil
}

func newToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeToken(path, token string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(token+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0o600)
}
