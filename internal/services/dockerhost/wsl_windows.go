package dockerhost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
)

// ErrWSLCommand reports a wsl.exe call that failed or answered unexpectedly.
var ErrWSLCommand = errors.New("wsl command failed")

// WSLDistribution is one installed WSL distro.
type WSLDistribution struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Default bool   `json:"default"`
}

// States of Docker inside a WSL distro.
const (
	WSLDockerReady      = "ready"
	WSLDockerNeedsSetup = "needs_setup"
	WSLDockerSystemdOff = "systemd_off"
	WSLDockerMissing    = "docker_missing"
)

// WSLDockerStatus is the state of Docker inside one distro. Commands is the
// script the setup runs as root.
type WSLDockerStatus struct {
	Distribution string `json:"distribution"`
	State        string `json:"state" enums:"ready,needs_setup,systemd_off,docker_missing"`
	Commands     string `json:"commands"`
}

// wslCommand runs wsl.exe with the arguments and stdin and returns its
// decoded output. Tests replace it.
var wslCommand = runWSL

// wslSetupMu serializes WSL Docker setups.
var wslSetupMu sync.Mutex

// wslCommandTimeout bounds one wsl.exe call. The first call after a distro
// stopped boots it.
const wslCommandTimeout = 2 * time.Minute

// The drop-in adds the loopback listener to docker.socket. dockerd runs with
// -H fd:// and serves every socket systemd passes, so the unix socket stays
// and no daemon flag changes.
const (
	wslDockerDropInDirectory = "/etc/systemd/system/docker.socket.d"
	wslDockerDropIn          = wslDockerDropInDirectory + "/llm-router.conf"
)

// wslDockerAddress is where Docker listens inside the distro. WSL forwards
// it to the loopback of Windows.
const (
	wslDockerAddress  = "127.0.0.1:2375"
	wslDockerEndpoint = "tcp://" + wslDockerAddress
)

var wslDockerSetupScript = `set -eu
mkdir -p ` + wslDockerDropInDirectory + `
printf '[Socket]\nListenStream=` + wslDockerAddress + `\n' > ` + wslDockerDropIn + `
systemctl daemon-reload
systemctl enable docker.socket docker.service
systemctl stop docker.service docker.socket
systemctl start docker.socket docker.service
`

var wslDockerProbeScript = `command -v dockerd >/dev/null || { echo ` + WSLDockerMissing + `; exit 0; }
[ -d /run/systemd/system ] || { echo ` + WSLDockerSystemdOff + `; exit 0; }
if [ -f ` + wslDockerDropIn + ` ] && systemctl is-active --quiet docker.socket; then echo ` + WSLDockerReady + `; else echo ` + WSLDockerNeedsSetup + `; fi
`

var wslDistributionNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

func runWSL(ctx context.Context, stdin string, arguments ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, wslCommandTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "wsl.exe", arguments...)
	command.Stdin = strings.NewReader(stdin)
	output, err := command.CombinedOutput()
	text := decodeWSLOutput(output)
	if err != nil {
		return "", fmt.Errorf("%w: wsl.exe %s: %v: %s", ErrWSLCommand, strings.Join(arguments, " "), err, strings.TrimSpace(text))
	}
	return text, nil
}

// decodeWSLOutput decodes the UTF-16LE that wsl.exe writes for its own
// messages. Output of Linux processes passes through as UTF-8.
func decodeWSLOutput(raw []byte) string {
	if bytes.IndexByte(raw, 0) < 0 {
		return string(raw)
	}
	raw = bytes.TrimPrefix(raw, []byte{0xFF, 0xFE})
	units := make([]uint16, 0, len(raw)/2)
	for index := 0; index+1 < len(raw); index += 2 {
		units = append(units, uint16(raw[index])|uint16(raw[index+1])<<8)
	}
	return string(utf16.Decode(units))
}

// parseWSLDistributions reads `wsl.exe --list --verbose`. The header and the
// state column are localized, so only the name, the default mark and the
// trailing version are read. Docker Desktop's own distros are left out.
func parseWSLDistributions(output string) ([]WSLDistribution, error) {
	lines := strings.Split(strings.ReplaceAll(output, "\r", ""), "\n")
	distributions := []WSLDistribution{}
	header := true
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if header {
			header = false
			continue
		}
		isDefault := strings.HasPrefix(line, "*")
		fields := strings.Fields(strings.TrimPrefix(line, "*"))
		if len(fields) < 3 {
			return nil, fmt.Errorf("%w: unexpected distro line %q", ErrWSLCommand, line)
		}
		version, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			return nil, fmt.Errorf("%w: unexpected distro line %q", ErrWSLCommand, line)
		}
		if strings.HasPrefix(fields[0], "docker-desktop") {
			continue
		}
		distributions = append(distributions, WSLDistribution{Name: fields[0], Version: version, Default: isDefault})
	}
	return distributions, nil
}

// enableSystemdInWSLConfiguration sets systemd=true in the [boot] section of
// a wsl.conf and keeps every other line.
func enableSystemdInWSLConfiguration(content string) string {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	section := ""
	bootHeader := -1
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			if section == "boot" && bootHeader < 0 {
				bootHeader = index
			}
			continue
		}
		key, _, found := strings.Cut(trimmed, "=")
		if section == "boot" && found && strings.EqualFold(strings.TrimSpace(key), "systemd") {
			lines[index] = "systemd=true"
			return strings.Join(lines, "\n")
		}
	}
	if bootHeader >= 0 {
		lines = append(lines[:bootHeader+1], append([]string{"systemd=true"}, lines[bootHeader+1:]...)...)
		return strings.Join(lines, "\n")
	}
	text := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if text != "" {
		text += "\n\n"
	}
	return text + "[boot]\nsystemd=true\n"
}

// WSLDistributions lists the installed WSL distros.
func (service *Service) WSLDistributions(ctx context.Context) ([]WSLDistribution, error) {
	output, err := wslCommand(ctx, "", "--list", "--verbose")
	if err != nil {
		return nil, err
	}
	return parseWSLDistributions(output)
}

// validateWSLDistribution checks the distro a connection keeps running: a
// valid name, and only with the WSL endpoint.
func validateWSLDistribution(endpoint, name string) error {
	if name == "" {
		return nil
	}
	if !wslDistributionNamePattern.MatchString(name) {
		return fmt.Errorf("%w: distro name %q", ErrInvalidSettings, name)
	}
	if endpoint != wslDockerEndpoint {
		return fmt.Errorf("%w: wsl_distribution needs the endpoint %s", ErrInvalidSettings, wslDockerEndpoint)
	}
	return nil
}

// wslDistribution finds one distro that can run the Docker setup.
func (service *Service) wslDistribution(ctx context.Context, name string) (WSLDistribution, error) {
	if !wslDistributionNamePattern.MatchString(name) {
		return WSLDistribution{}, fmt.Errorf("%w: distro name %q", ErrInvalidSettings, name)
	}
	distributions, err := service.WSLDistributions(ctx)
	if err != nil {
		return WSLDistribution{}, err
	}
	for _, distribution := range distributions {
		if distribution.Name != name {
			continue
		}
		if distribution.Version != 2 {
			return WSLDistribution{}, fmt.Errorf("%w: distro %s runs on WSL %d: Docker needs WSL 2", ErrInvalidSettings, name, distribution.Version)
		}
		return distribution, nil
	}
	return WSLDistribution{}, fmt.Errorf("WSL distro %q: %w", name, apierrors.ErrNotFound)
}

// runAsRoot runs a shell script as root inside a distro.
func (service *Service) runAsRoot(ctx context.Context, name, script string) (string, error) {
	return wslCommand(ctx, script, "--distribution", name, "--user", "root", "--exec", "sh", "-s")
}

func (service *Service) probeWSLDocker(ctx context.Context, name string) (WSLDockerStatus, error) {
	output, err := service.runAsRoot(ctx, name, wslDockerProbeScript)
	if err != nil {
		return WSLDockerStatus{}, err
	}
	state := strings.TrimSpace(output)
	switch state {
	case WSLDockerReady, WSLDockerNeedsSetup, WSLDockerSystemdOff, WSLDockerMissing:
		return WSLDockerStatus{Distribution: name, State: state, Commands: wslDockerSetupScript}, nil
	default:
		return WSLDockerStatus{}, fmt.Errorf("%w: unexpected Docker state %q in %s", ErrWSLCommand, state, name)
	}
}

// WSLDocker reports the state of Docker inside a distro. It starts a
// stopped distro.
func (service *Service) WSLDocker(ctx context.Context, name string) (WSLDockerStatus, error) {
	if _, err := service.wslDistribution(ctx, name); err != nil {
		return WSLDockerStatus{}, err
	}
	return service.probeWSLDocker(ctx, name)
}

// SetUpWSLDocker makes Docker inside a distro a service that also listens on
// the loopback. It turns systemd on first when it is off, which restarts the
// distro. The Docker restart stops running containers of the distro.
func (service *Service) SetUpWSLDocker(ctx context.Context, name string) (WSLDockerStatus, error) {
	wslSetupMu.Lock()
	defer wslSetupMu.Unlock()
	status, err := service.WSLDocker(ctx, name)
	if err != nil {
		return WSLDockerStatus{}, err
	}
	switch status.State {
	case WSLDockerReady:
		return status, nil
	case WSLDockerMissing:
		return WSLDockerStatus{}, fmt.Errorf("%w: Docker Engine is not installed in %s", ErrInvalidSettings, name)
	case WSLDockerSystemdOff:
		if err := service.enableWSLSystemd(ctx, name); err != nil {
			return WSLDockerStatus{}, err
		}
	}
	if _, err := service.runAsRoot(ctx, name, wslDockerSetupScript); err != nil {
		return WSLDockerStatus{}, err
	}
	status, err = service.probeWSLDocker(ctx, name)
	if err != nil {
		return WSLDockerStatus{}, err
	}
	if status.State != WSLDockerReady {
		return WSLDockerStatus{}, fmt.Errorf("%w: setup finished, but Docker in %s is %s", ErrWSLCommand, name, status.State)
	}
	service.logInfo("WSL Docker set up", "distribution", name)
	return status, nil
}

// enableWSLSystemd writes systemd=true to /etc/wsl.conf and terminates the
// distro so the next call boots it with systemd.
func (service *Service) enableWSLSystemd(ctx context.Context, name string) error {
	current, err := service.runAsRoot(ctx, name, "if [ -e /etc/wsl.conf ]; then cat /etc/wsl.conf; fi\n")
	if err != nil {
		return err
	}
	updated := enableSystemdInWSLConfiguration(current)
	if _, err := wslCommand(ctx, updated, "--distribution", name, "--user", "root", "--exec", "sh", "-c", "cat > /etc/wsl.conf"); err != nil {
		return err
	}
	if _, err := wslCommand(ctx, "", "--terminate", name); err != nil {
		return err
	}
	service.logInfo("WSL systemd turned on", "distribution", name)
	return nil
}
