package record

import (
	"time"

	// Registers the credential_cipher serializer used by the secret columns.
	_ "github.com/TheSlopMachine/llm-router/internal/credentialcipher"
)

// DockerConnection is the single Docker daemon connection row. The
// identifier is always 1. An empty endpoint means Docker is unconfigured.
type DockerConnection struct {
	ID           int64                   `gorm:"primaryKey;check:id = 1"`
	Endpoint     string                  `gorm:"not null"`
	Namespace    string                  `gorm:"not null"`
	DialMode     string                  `gorm:"not null"`
	GatewayImage string                  `gorm:"not null"`
	SSHHostKey   string                  `gorm:"column:ssh_host_key;not null"`
	Secrets      DockerConnectionSecrets `gorm:"serializer:credential_cipher;not null"`
	UpdatedAt    time.Time               `gorm:"not null"`
}

// TableName pins the docker connections table name.
func (DockerConnection) TableName() string {
	return "docker_connections"
}

// DockerConnectionSecrets is the sealed part of the connection: TLS and SSH
// material in PEM form.
type DockerConnectionSecrets struct {
	TLSCA          string `json:"tls_ca,omitempty"`
	TLSCertificate string `json:"tls_certificate,omitempty"`
	TLSKey         string `json:"tls_key,omitempty"`
	SSHPrivateKey  string `json:"ssh_private_key,omitempty"`
	SSHPassphrase  string `json:"ssh_passphrase,omitempty"`
}

// DockerRegistryCredential is one registry login. Host is the registry
// host as image references name it ("docker.io" for Docker Hub).
type DockerRegistryCredential struct {
	Host      string    `gorm:"primaryKey"`
	Username  string    `gorm:"not null"`
	Secret    string    `gorm:"serializer:credential_cipher;not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

// TableName pins the docker registry credentials table name.
func (DockerRegistryCredential) TableName() string {
	return "docker_registry_credentials"
}
