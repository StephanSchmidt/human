//go:build darwin

package browser

import "os/exec"

func startBrowser(url string) error {
	return exec.Command("open", url).Start()
}
