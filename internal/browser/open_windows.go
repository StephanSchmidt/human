//go:build windows

package browser

import "os/exec"

func startBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
