//go:build windows

package cmd

import (
	"context"
	"os"
	"testing"
)

func TestShutdownContextRefusesExistingStopEvent(t *testing.T) {
	_, release, err := shutdownContext(context.Background())
	if err != nil {
		t.Fatalf("shutdownContext: %v", err)
	}
	defer release()
	if _, _, err := shutdownContext(context.Background()); err == nil {
		t.Fatal("second shutdownContext succeeded with the stop event in place")
	}
}

func TestShutdownContextReleaseRemovesStopEvent(t *testing.T) {
	ctx, release, err := shutdownContext(context.Background())
	if err != nil {
		t.Fatalf("shutdownContext: %v", err)
	}
	release()
	if ctx.Err() == nil {
		t.Error("release did not end the context")
	}
	if err := requestStop(os.Getpid()); err == nil {
		t.Error("requestStop succeeded after release")
	}
}
