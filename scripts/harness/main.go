// Command harness validates the router against one CLI harness: three resumed
// phases through the router, answered by the harness-mock plugin, then checks of
// the marker files and of the plugin's phase events. Run it with make harness
// HARNESS=claude|codex|opencode against a running stack with the plugin uploaded.
package main

import (
	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

func main() {
	name := shared.RequireEnv("HARNESS")
	driver, err := driverNamed(name)
	if err != nil {
		shared.Failf("%v", err)
	}
	if err := runHarness(driver); err != nil {
		shared.Failf("%s: %v", name, err)
	}
}
