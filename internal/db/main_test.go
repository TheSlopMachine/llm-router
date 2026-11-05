package db_test

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/TheSlopMachine/llm-router/internal/credentialcipher"
)

// TestMain installs a credential key once: these tests open databases
// directly, and credential rows refuse to persist without a key.
func TestMain(m *testing.M) {
	if err := credentialcipher.Install(bytes.Repeat([]byte{0x3c}, credentialcipher.KeySize)); err != nil {
		fmt.Fprintln(os.Stderr, "install test credential key:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
