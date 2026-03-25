package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/spf13/afero"
)

// DaemonInfo holds the runtime details of a running daemon instance.
type DaemonInfo struct {
	Addr       string `json:"addr"`
	ChromeAddr string `json:"chrome_addr,omitempty"`
	ProxyAddr  string `json:"proxy_addr,omitempty"`
	Token      string `json:"token"`
	PID        int    `json:"pid"`
}

// InfoPath returns the default path for the daemon info file (~/.human/daemon.json).
func InfoPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".human", "daemon.json")
	}
	return filepath.Join(home, ".human", "daemon.json")
}

// WriteInfo writes the daemon info as JSON to InfoPath with restricted permissions.
func WriteInfo(info DaemonInfo) error {
	path := InfoPath()
	if err := fs.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return afero.WriteFile(fs, path, data, 0o600)
}

// ReadInfo reads and unmarshals the daemon info from InfoPath.
func ReadInfo() (DaemonInfo, error) {
	data, err := afero.ReadFile(fs, InfoPath())
	if err != nil {
		return DaemonInfo{}, err
	}
	var info DaemonInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return DaemonInfo{}, err
	}
	return info, nil
}

// RemoveInfo removes the daemon info file (best-effort).
func RemoveInfo() {
	_ = fs.Remove(InfoPath())
}
