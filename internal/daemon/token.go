package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

const tokenBytes = 32

// fs is the filesystem used by token operations. Tests can swap this with afero.NewMemMapFs().
var fs afero.Fs = afero.NewOsFs()

// GenerateToken returns a cryptographically random 32-byte hex string.
func GenerateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// TokenPath returns the default path for the daemon token file.
func TokenPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.TempDir(), ".config")
	}
	return filepath.Join(dir, "human", "daemon-token")
}

// LoadOrCreateToken reads the token from disk, or generates and persists a new one.
func LoadOrCreateToken() (string, error) {
	return loadOrCreateTokenAt(TokenPath())
}

func loadOrCreateTokenAt(path string) (string, error) {
	data, err := afero.ReadFile(fs, path)
	if err == nil {
		token := string(data)
		if len(token) == tokenBytes*2 {
			return token, nil
		}
	}

	token, err := GenerateToken()
	if err != nil {
		return "", err
	}

	if err := fs.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := afero.WriteFile(fs, path, []byte(token), 0o600); err != nil {
		return "", err
	}
	return token, nil
}
