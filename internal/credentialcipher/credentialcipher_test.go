package credentialcipher

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	cipher, err := New(bytes.Repeat([]byte{7}, KeySize))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := cipher.Seal([]byte(`{"api_key":"sk-secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(sealed, SealedPrefix) || strings.Contains(sealed, "sk-secret") {
		t.Fatalf("sealed value leaks plaintext or misses the prefix: %q", sealed)
	}
	again, _ := cipher.Seal([]byte(`{"api_key":"sk-secret"}`))
	if again == sealed {
		t.Fatal("nonces must differ between seals")
	}
	opened, err := cipher.Open(sealed)
	if err != nil || string(opened) != `{"api_key":"sk-secret"}` {
		t.Fatalf("open: %q %v", opened, err)
	}
	if _, err := cipher.Open(`{"api_key":"plain"}`); !errors.Is(err, ErrNotSealed) {
		t.Fatalf("plaintext must report ErrNotSealed, got %v", err)
	}
	other, _ := New(bytes.Repeat([]byte{8}, KeySize))
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("a different key must fail to open")
	}
}

func TestInstallConfiguredCreatesKeyFileOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "credentials.key")
	created, err := InstallConfigured("", path)
	if err != nil || !created {
		t.Fatalf("first install must create the key file: created=%v err=%v", created, err)
	}
	first, _ := os.ReadFile(path)
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("key file mode = %v, want 0600", info.Mode().Perm())
		}
	}
	created, err = InstallConfigured("", path)
	if err != nil || created {
		t.Fatalf("second install must reuse the file: created=%v err=%v", created, err)
	}
	second, _ := os.ReadFile(path)
	if !bytes.Equal(first, second) {
		t.Fatal("existing key file must not change")
	}
}

func TestInstallConfiguredPrefersEnvironmentKey(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{9}, KeySize))
	path := filepath.Join(t.TempDir(), "credentials.key")
	if _, err := InstallConfigured(key, path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("an environment key must not create the key file")
	}
	if _, err := InstallConfigured("short", path); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("invalid key must fail, got %v", err)
	}
}
