package dockerhost

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/db"
	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/dockerengine"
	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
)

// connectionRowID pins the single docker_connections row.
const connectionRowID int64 = 1

// Dial modes of container port connections.
const (
	DialModeAuto  = "auto"
	DialModeRoute = "route"
	DialModeRelay = "relay"
)

// Ways to reach Docker, as the setup wizard offers them. They match the
// endpoint schemes.
const (
	ConnectionSSH  = string(dockerengine.EndpointSSH)
	ConnectionUnix = string(dockerengine.EndpointUnix)
	ConnectionTCP  = string(dockerengine.EndpointTCP)
)

// DefaultNamespace prefixes every object name and labels every object.
const DefaultNamespace = "llm-router"

var namespacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,31}$`)

// ErrInvalidSettings reports a connection or registry setting the service
// rejects.
var ErrInvalidSettings = errors.New("invalid docker settings")

// ConnectionSettings is the dashboard view of the connection. Secrets show
// only whether they are set.
type ConnectionSettings struct {
	Endpoint              string `json:"endpoint"`
	Namespace             string `json:"namespace"`
	DialMode              string `json:"dial_mode"`
	GatewayImage          string `json:"gateway_image"`
	SSHHostKey            string `json:"ssh_host_key"`
	SSHHostKeyFingerprint string `json:"ssh_host_key_fingerprint"`
	HasTLSCA              bool   `json:"has_tls_ca"`
	HasTLSCertificate     bool   `json:"has_tls_certificate"`
	HasTLSKey             bool   `json:"has_tls_key"`
	HasSSHPrivateKey      bool   `json:"has_ssh_private_key"`
	HasSSHPassphrase      bool   `json:"has_ssh_passphrase"`
	// WSLDistribution is the WSL distro the router keeps running, empty
	// outside WSL setups.
	WSLDistribution string `json:"wsl_distribution"`
	// ConnectionKinds are the ways this router reaches Docker. They depend
	// on the platform the router runs on.
	ConnectionKinds []string `json:"connection_kinds"`
}

// ConnectionUpdate changes the connection. A nil secret keeps the stored
// value; an empty string clears it.
type ConnectionUpdate struct {
	Endpoint     string `json:"endpoint"`
	Namespace    string `json:"namespace"`
	DialMode     string `json:"dial_mode"`
	GatewayImage string `json:"gateway_image"`
	SSHHostKey   string `json:"ssh_host_key"`
	// WSLDistribution names the WSL distro to keep running. It needs the
	// WSL endpoint on a Windows router.
	WSLDistribution string  `json:"wsl_distribution"`
	TLSCA           *string `json:"tls_ca"`
	TLSCertificate  *string `json:"tls_certificate"`
	TLSKey          *string `json:"tls_key"`
	SSHPrivateKey   *string `json:"ssh_private_key"`
	SSHPassphrase   *string `json:"ssh_passphrase"`
}

// RegistryCredential is the dashboard view of one registry login.
type RegistryCredential struct {
	Host      string    `json:"host"`
	Username  string    `json:"username"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RegistryCredentialUpdate stores one registry login.
type RegistryCredentialUpdate struct {
	Username string `json:"username"`
	Secret   string `json:"secret"`
}

var registryHostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)

func defaultConnectionRow() record.DockerConnection {
	return record.DockerConnection{ID: connectionRowID, Namespace: DefaultNamespace, DialMode: DialModeAuto}
}

func (service *Service) loadConnectionRow(ctx context.Context, handle *gorm.DB) (record.DockerConnection, error) {
	row, err := gorm.G[record.DockerConnection](handle, clause.Eq{Column: "id", Value: connectionRowID}).First(ctx)
	if err != nil {
		if errors.Is(db.Translate(err), apierrors.ErrNotFound) {
			return defaultConnectionRow(), nil
		}
		return record.DockerConnection{}, fmt.Errorf("read docker connection: %w", db.Translate(err))
	}
	return row, nil
}

func connectionView(row record.DockerConnection) ConnectionSettings {
	view := ConnectionSettings{
		Endpoint:          row.Endpoint,
		Namespace:         row.Namespace,
		DialMode:          row.DialMode,
		GatewayImage:      row.GatewayImage,
		SSHHostKey:        row.SSHHostKey,
		HasTLSCA:          row.Secrets.TLSCA != "",
		HasTLSCertificate: row.Secrets.TLSCertificate != "",
		HasTLSKey:         row.Secrets.TLSKey != "",
		HasSSHPrivateKey:  row.Secrets.SSHPrivateKey != "",
		HasSSHPassphrase:  row.Secrets.SSHPassphrase != "",
		WSLDistribution:   row.WSLDistribution,
		ConnectionKinds:   connectionKinds,
	}
	if row.SSHHostKey != "" {
		if fingerprint, err := dockerengine.HostKeyFingerprint(row.SSHHostKey); err == nil {
			view.SSHHostKeyFingerprint = fingerprint
		}
	}
	return view
}

// Connection returns the stored connection settings.
func (service *Service) Connection(ctx context.Context) (ConnectionSettings, error) {
	row, err := service.loadConnectionRow(ctx, service.database)
	if err != nil {
		return ConnectionSettings{}, err
	}
	return connectionView(row), nil
}

// validateEndpoint parses an endpoint and checks the platform supports its
// topology. Empty means unconfigured.
func validateEndpoint(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	endpoint, err := dockerengine.ParseEndpoint(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSettings, err)
	}
	if !slices.Contains(connectionKinds, string(endpoint.Kind)) {
		return fmt.Errorf("%w: %s endpoints are not supported on %s", ErrInvalidSettings, endpoint.Kind, runtime.GOOS)
	}
	return nil
}

func applySecret(target *string, update *string) {
	if update != nil {
		*target = strings.TrimSpace(*update)
	}
}

// PutConnection validates and stores the connection, then drops the live
// session so the next call connects with the new settings. Removing the
// endpoint runs the connection removed hook.
func (service *Service) PutConnection(ctx context.Context, update ConnectionUpdate) (ConnectionSettings, error) {
	update.Endpoint = strings.TrimSpace(update.Endpoint)
	if err := validateEndpoint(update.Endpoint); err != nil {
		return ConnectionSettings{}, err
	}
	if update.Namespace == "" {
		update.Namespace = DefaultNamespace
	}
	if !namespacePattern.MatchString(update.Namespace) {
		return ConnectionSettings{}, fmt.Errorf("%w: namespace must match %s", ErrInvalidSettings, namespacePattern.String())
	}
	switch update.DialMode {
	case "":
		update.DialMode = DialModeAuto
	case DialModeAuto, DialModeRoute, DialModeRelay:
	default:
		return ConnectionSettings{}, fmt.Errorf("%w: dial_mode must be auto, route or relay", ErrInvalidSettings)
	}
	update.GatewayImage = strings.TrimSpace(update.GatewayImage)
	if update.GatewayImage != "" && !imageReferencePattern.MatchString(update.GatewayImage) {
		return ConnectionSettings{}, fmt.Errorf("%w: gateway_image is not an image reference", ErrInvalidSettings)
	}
	update.SSHHostKey = strings.TrimSpace(update.SSHHostKey)
	if update.SSHHostKey != "" {
		if _, err := dockerengine.ParseHostKey(update.SSHHostKey); err != nil {
			return ConnectionSettings{}, fmt.Errorf("%w: %v", ErrInvalidSettings, err)
		}
	}
	update.WSLDistribution = strings.TrimSpace(update.WSLDistribution)
	if err := validateWSLDistribution(update.Endpoint, update.WSLDistribution); err != nil {
		return ConnectionSettings{}, err
	}
	var stored record.DockerConnection
	err := service.database.Transaction(func(transaction *gorm.DB) error {
		row, err := service.loadConnectionRow(ctx, transaction)
		if err != nil {
			return err
		}
		row.Endpoint = update.Endpoint
		row.Namespace = update.Namespace
		row.DialMode = update.DialMode
		row.GatewayImage = update.GatewayImage
		row.SSHHostKey = update.SSHHostKey
		row.WSLDistribution = update.WSLDistribution
		applySecret(&row.Secrets.TLSCA, update.TLSCA)
		applySecret(&row.Secrets.TLSCertificate, update.TLSCertificate)
		applySecret(&row.Secrets.TLSKey, update.TLSKey)
		applySecret(&row.Secrets.SSHPrivateKey, update.SSHPrivateKey)
		applySecret(&row.Secrets.SSHPassphrase, update.SSHPassphrase)
		row.UpdatedAt = time.Now().UTC()
		if err := gorm.G[record.DockerConnection](transaction, clause.OnConflict{Columns: []clause.Column{{Name: "id"}}, UpdateAll: true}).Create(ctx, &row); err != nil {
			return fmt.Errorf("store docker connection: %w", db.Translate(err))
		}
		stored = row
		return nil
	})
	if err != nil {
		return ConnectionSettings{}, err
	}
	service.resetSession()
	service.keepWSLDistribution(stored.WSLDistribution)
	service.logInfo("Docker connection updated", "endpoint", update.Endpoint, "dial_mode", update.DialMode)
	if update.Endpoint == "" {
		service.wiringMu.RLock()
		hook := service.connectionRemoved
		service.wiringMu.RUnlock()
		if hook != nil {
			if err := hook(ctx); err != nil {
				return ConnectionSettings{}, fmt.Errorf("docker host removed, but disabling its plugins failed: %w", err)
			}
		}
	}
	return connectionView(stored), nil
}

// connectorOptions builds transport options from the stored row.
func connectorOptions(row record.DockerConnection) (dockerengine.Options, error) {
	endpoint, err := dockerengine.ParseEndpoint(row.Endpoint)
	if err != nil {
		return dockerengine.Options{}, err
	}
	return dockerengine.Options{
		Endpoint: endpoint,
		TLS: dockerengine.TLSMaterial{
			CA:          []byte(row.Secrets.TLSCA),
			Certificate: []byte(row.Secrets.TLSCertificate),
			Key:         []byte(row.Secrets.TLSKey),
		},
		SSH: dockerengine.SSHMaterial{
			PrivateKey: []byte(row.Secrets.SSHPrivateKey),
			Passphrase: []byte(row.Secrets.SSHPassphrase),
			HostKey:    row.SSHHostKey,
		},
	}, nil
}

// HostKeyScan is the answer of a host key scan.
type HostKeyScan struct {
	HostKey     string `json:"host_key"`
	Fingerprint string `json:"fingerprint"`
}

// ScanHostKey reads the SSH host key of an endpoint for operator
// confirmation. It stores nothing.
func (service *Service) ScanHostKey(ctx context.Context, rawEndpoint string) (HostKeyScan, error) {
	endpoint, err := dockerengine.ParseEndpoint(rawEndpoint)
	if err != nil {
		return HostKeyScan{}, fmt.Errorf("%w: %v", ErrInvalidSettings, err)
	}
	key, fingerprint, err := dockerengine.ScanHostKey(ctx, endpoint)
	if err != nil {
		return HostKeyScan{}, err
	}
	return HostKeyScan{HostKey: key, Fingerprint: fingerprint}, nil
}

// RegistryCredentials lists registry logins without secrets.
func (service *Service) RegistryCredentials(ctx context.Context) ([]RegistryCredential, error) {
	rows, err := gorm.G[record.DockerRegistryCredential](service.database, clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "host"}}}}).Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("list registry credentials: %w", db.Translate(err))
	}
	views := make([]RegistryCredential, 0, len(rows))
	for _, row := range rows {
		views = append(views, RegistryCredential{Host: row.Host, Username: row.Username, UpdatedAt: row.UpdatedAt})
	}
	return views, nil
}

// PutRegistryCredential stores one registry login.
func (service *Service) PutRegistryCredential(ctx context.Context, host string, update RegistryCredentialUpdate) (RegistryCredential, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	if !registryHostPattern.MatchString(host) {
		return RegistryCredential{}, fmt.Errorf("%w: registry host %q must be a host name such as docker.io or ghcr.io", ErrInvalidSettings, host)
	}
	update.Username = strings.TrimSpace(update.Username)
	if update.Username == "" || update.Secret == "" {
		return RegistryCredential{}, fmt.Errorf("%w: username and secret are required", ErrInvalidSettings)
	}
	row := record.DockerRegistryCredential{Host: host, Username: update.Username, Secret: update.Secret, UpdatedAt: time.Now().UTC()}
	if err := gorm.G[record.DockerRegistryCredential](service.database, clause.OnConflict{Columns: []clause.Column{{Name: "host"}}, UpdateAll: true}).Create(ctx, &row); err != nil {
		return RegistryCredential{}, fmt.Errorf("store registry credential: %w", db.Translate(err))
	}
	service.logInfo("Docker registry credential stored", "host", host)
	return RegistryCredential{Host: host, Username: row.Username, UpdatedAt: row.UpdatedAt}, nil
}

// DeleteRegistryCredential removes one registry login.
func (service *Service) DeleteRegistryCredential(ctx context.Context, host string) error {
	affected, err := gorm.G[record.DockerRegistryCredential](service.database, clause.Eq{Column: "host", Value: strings.ToLower(strings.TrimSpace(host))}).Delete(ctx)
	if err != nil {
		return fmt.Errorf("delete registry credential: %w", db.Translate(err))
	}
	if affected == 0 {
		return fmt.Errorf("registry credential %q: %w", host, apierrors.ErrNotFound)
	}
	return nil
}

// registryLogins returns every stored login keyed by host.
func (service *Service) registryLogins(ctx context.Context) (map[string]record.DockerRegistryCredential, error) {
	rows, err := gorm.G[record.DockerRegistryCredential](service.database).Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("read registry credentials: %w", db.Translate(err))
	}
	logins := make(map[string]record.DockerRegistryCredential, len(rows))
	for _, row := range rows {
		logins[row.Host] = row
	}
	return logins, nil
}

// sortedKeys returns the keys of a string map in order.
func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
