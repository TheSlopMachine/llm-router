package main

import (
	"os/exec"
	"runtime"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	host := shared.Getenv("HOST", "localhost")
	webPort := shared.Getenv("WEB_PORT", "8080")
	url := shared.Getenv("URL", "http://"+host+":"+webPort)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	shared.Stepf("Opening %s...", url)
	if err := cmd.Start(); err != nil {
		shared.Failf("open browser: %v", err)
	}
	shared.OKf("Opened %s", url)
}
