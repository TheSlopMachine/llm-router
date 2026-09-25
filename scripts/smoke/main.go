package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// Exit codes: 0 clean (skips allowed), 1 real failure, 2 harness error.
func main() {
	cfg, err := loadConfig()
	if err != nil {
		shared.Failf("%v", err)
	}
	if err := waitReady(cfg); err != nil {
		shared.Failf("%v", err)
	}
	rep := &report{}
	for _, pluginType := range cfg.plugins {
		if !knownTypes[pluginType] {
			shared.Failf("unknown plugin type %q", pluginType)
		}
		runPlugin(cfg, rep, pluginType)
	}
	rep.print()
	if rep.failed() {
		os.Exit(1)
	}
	shared.OKf("smoke clean")
}

func runPlugin(cfg config, rep *report, pluginType string) {
	providerID, cleanup, err := provision(cfg, pluginType)
	if err != nil {
		if errors.Is(err, errNoAccount) {
			rep.add(pluginType, "-", "-", skip, "no credential in db", 0)
			return
		}
		rep.add(pluginType, "-", "-", fail, fmt.Sprintf("provision: %v", err), 0)
		return
	}
	if cleanup != nil {
		defer cleanup()
	}
	models, err := listModels(cfg, providerID)
	if err != nil {
		rep.add(pluginType, "-", "-", fail, fmt.Sprintf("models: %v", err), 0)
		return
	}
	if len(models) == 0 {
		rep.add(pluginType, "-", "-", fail, "no models listed", 0)
		return
	}
	runMatrix(cfg, rep, pluginType, providerID, models)
}
