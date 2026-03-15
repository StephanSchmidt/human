//go:build linux

package browser

import "os/exec"

func startBrowser(url string) error {
	return exec.Command("xdg-open", url).Start() // #nosec G204 -- url is validated by ValidateURL (http/https only)
}
