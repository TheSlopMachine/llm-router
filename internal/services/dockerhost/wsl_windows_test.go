package dockerhost

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/testutil"
)

const wslListing = "  NAME              STATE           VERSION\r\n* Ubuntu-24.04      Running         2\r\n  Debian            Stopped         2\r\n  Ubuntu-20.04      Stopped         1\r\n  docker-desktop    Stopped         2\r\n"

func utf16Bytes(text string) []byte {
	raw := []byte{0xFF, 0xFE}
	for _, unit := range utf16.Encode([]rune(text)) {
		raw = append(raw, byte(unit), byte(unit>>8))
	}
	return raw
}

// TestWSLDistributionListing guards the listing parse: UTF-16 output, the
// default mark, the version column and Docker Desktop distros left out.
func TestWSLDistributionListing(t *testing.T) {
	decoded := decodeWSLOutput(utf16Bytes(wslListing))
	if decoded != wslListing {
		t.Fatalf("decode: %q", decoded)
	}
	if plain := decodeWSLOutput([]byte("ready\n")); plain != "ready\n" {
		t.Fatalf("utf-8 output: %q", plain)
	}
	distributions, err := parseWSLDistributions(decoded)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []WSLDistribution{
		{Name: "Ubuntu-24.04", Version: 2, Default: true},
		{Name: "Debian", Version: 2},
		{Name: "Ubuntu-20.04", Version: 1},
	}
	if !reflect.DeepEqual(distributions, want) {
		t.Fatalf("parse: %+v", distributions)
	}
	if _, err := parseWSLDistributions("NAME STATE VERSION\nbroken line\n"); !errors.Is(err, ErrWSLCommand) {
		t.Fatalf("broken line: %v", err)
	}
}

// TestEnableSystemdInWSLConfiguration guards the wsl.conf edit: it sets the
// key in [boot] and keeps every other line.
func TestEnableSystemdInWSLConfiguration(t *testing.T) {
	cases := []struct{ name, input, want string }{
		{"empty", "", "[boot]\nsystemd=true\n"},
		{"other section", "[network]\nhostname=box\n", "[network]\nhostname=box\n\n[boot]\nsystemd=true\n"},
		{"boot without key", "[boot]\ncommand=echo hi\n", "[boot]\nsystemd=true\ncommand=echo hi\n"},
		{"key off", "[boot]\nsystemd = false\n[user]\ndefault=me\n", "[boot]\nsystemd=true\n[user]\ndefault=me\n"},
		{"key in another section", "[user]\nsystemd=false\n", "[user]\nsystemd=false\n\n[boot]\nsystemd=true\n"},
	}
	for _, testCase := range cases {
		if got := enableSystemdInWSLConfiguration(testCase.input); got != testCase.want {
			t.Fatalf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// fakeWSL answers wsl.exe calls from a distro state and records them.
type fakeWSL struct {
	state      string
	wslConf    string
	calls      []string
	setupFails bool
}

func (fake *fakeWSL) run(_ context.Context, stdin string, arguments ...string) (string, error) {
	line := strings.Join(arguments, " ")
	fake.calls = append(fake.calls, line)
	switch {
	case line == "--list --verbose":
		return wslListing, nil
	case line == "--terminate Ubuntu-24.04":
		if strings.Contains(fake.wslConf, "systemd=true") {
			fake.state = WSLDockerNeedsSetup
		}
		return "", nil
	case strings.HasSuffix(line, "--exec sh -c cat > /etc/wsl.conf"):
		fake.wslConf = stdin
		return "", nil
	case stdin == wslDockerProbeScript:
		return fake.state + "\n", nil
	case stdin == wslDockerSetupScript:
		if fake.setupFails {
			return "", ErrWSLCommand
		}
		fake.state = WSLDockerReady
		return "", nil
	case strings.Contains(stdin, "cat /etc/wsl.conf"):
		return fake.wslConf, nil
	}
	return "", errors.New("unexpected call: " + line)
}

// TestSetUpWSLDocker guards the setup flow: systemd turns on before the
// Docker setup, a missing Docker and WSL 1 are refused, a failed script
// surfaces.
func TestSetUpWSLDocker(t *testing.T) {
	service := New(testutil.SetupTestSQLite(t))
	fake := &fakeWSL{state: WSLDockerSystemdOff, wslConf: "[user]\ndefault=me\n"}
	wslCommand = fake.run
	t.Cleanup(func() { wslCommand = runWSL })
	status, err := service.SetUpWSLDocker(context.Background(), "Ubuntu-24.04")
	if err != nil || status.State != WSLDockerReady || status.Commands != wslDockerSetupScript {
		t.Fatalf("setup: %+v %v", status, err)
	}
	if fake.wslConf != "[user]\ndefault=me\n\n[boot]\nsystemd=true\n" {
		t.Fatalf("wsl.conf: %q", fake.wslConf)
	}
	calls := len(fake.calls)
	if _, err := service.SetUpWSLDocker(context.Background(), "Ubuntu-24.04"); err != nil || len(fake.calls) != calls+2 {
		t.Fatalf("ready distro reruns the setup: %v %v", err, fake.calls[calls:])
	}

	fake.state = WSLDockerMissing
	if _, err := service.SetUpWSLDocker(context.Background(), "Ubuntu-24.04"); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("missing docker: %v", err)
	}
	fake.state = WSLDockerNeedsSetup
	fake.setupFails = true
	if _, err := service.SetUpWSLDocker(context.Background(), "Ubuntu-24.04"); !errors.Is(err, ErrWSLCommand) {
		t.Fatalf("failed script: %v", err)
	}
	if _, err := service.WSLDocker(context.Background(), "Ubuntu-20.04"); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("wsl 1: %v", err)
	}
	if _, err := service.WSLDocker(context.Background(), "Fedora"); !errors.Is(err, apierrors.ErrNotFound) {
		t.Fatalf("unknown distro: %v", err)
	}
	if _, err := service.WSLDocker(context.Background(), "bad name"); !errors.Is(err, ErrInvalidSettings) {
		t.Fatalf("bad name: %v", err)
	}
}
