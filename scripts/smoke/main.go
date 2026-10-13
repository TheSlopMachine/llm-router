package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/TheSlopMachine/llm-router/scripts/shared"
)

// Exit codes: 0 clean (skips allowed), 1 real failure, 2 harness error.
func main() {
	testConfiguration, err := loadConfig()
	if err != nil {
		shared.Failf("%v", err)
	}
	requireRunningStack(testConfiguration)
	if err := waitReady(testConfiguration); err != nil {
		shared.Failf("%v", err)
	}
	cleanupStaleVirtualModels(testConfiguration)
	report := &report{}
	for _, pluginType := range testConfiguration.plugins {
		if !knownTypes[pluginType] {
			shared.Failf("unknown plugin type %q", pluginType)
		}
		runPlugin(testConfiguration, report, pluginType)
	}
	report.print()
	if report.failed() {
		os.Exit(1)
	}
	shared.OKf("smoke clean")
}

func runPlugin(testConfiguration configuration, report *report, pluginType string) {
	providerID, credentials, err := provision(testConfiguration, pluginType)
	if err != nil {
		if errors.Is(err, errNoCredential) {
			report.add(pluginType, "-", "-", skip, "no credential in db", 0)
			return
		}
		report.add(pluginType, "-", "-", fail, fmt.Sprintf("provision: %v", err), 0)
		return
	}
	models, err := listModels(testConfiguration, providerID)
	if err != nil {
		report.add(pluginType, "-", "-", fail, fmt.Sprintf("models: %v", err), 0)
		return
	}
	if len(models) == 0 {
		report.add(pluginType, "-", "-", fail, "no models listed", 0)
		return
	}
	checkCredentialTest(testConfiguration, report, pluginType, providerID, credentials, models)
	runMatrix(testConfiguration, report, pluginType, providerID, models)
}
