// Package credentialcipher encrypts stored credential data at rest with
// AES-256-GCM. One key serves the whole process: Install sets it before the
// databases open, and the credential_cipher GORM serializer seals every
// credentials.data write and opens every read. Without an installed key the
// serializer fails instead of writing plaintext.
package credentialcipher

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"

	"gorm.io/gorm/schema"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// SealedPrefix marks an encrypted value: the version selects the algorithm
// (v1 = AES-256-GCM, 12-byte nonce prepended to the ciphertext).
const SealedPrefix = "llmr-enc:v1:"

// SerializerName is the GORM serializer tag value for encrypted columns.
const SerializerName = "credential_cipher"

var (
	// ErrKeyMissing reports a seal or open attempt before Install.
	ErrKeyMissing = errors.New("credential encryption key is not installed")
	// ErrNotSealed reports a stored value without the sealed prefix.
	ErrNotSealed = errors.New("credential data is not encrypted")
	// ErrInvalidKey reports a key of the wrong length or encoding.
	ErrInvalidKey = errors.New("credential encryption key must be 32 bytes, base64-encoded")
)

// Cipher seals and opens credential payloads with one AES-256-GCM key.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a raw 32-byte key.
func New(key []byte) (*Cipher, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Seal encrypts plaintext into a prefixed, base64 text value.
func (c *Cipher) Seal(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return SealedPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Open decrypts a value produced by Seal.
func (c *Cipher) Open(value string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(value, SealedPrefix)
	if !ok {
		return nil, ErrNotSealed
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode sealed credential data: %w", err)
	}
	nonceSize := c.aead.NonceSize()
	if len(raw) < nonceSize {
		return nil, errors.New("sealed credential data is truncated")
	}
	plaintext, err := c.aead.Open(nil, raw[:nonceSize], raw[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential data (wrong key?): %w", err)
	}
	return plaintext, nil
}

var installed atomic.Pointer[Cipher]

// Install sets the process key. Later calls replace it.
func Install(key []byte) error {
	built, err := New(key)
	if err != nil {
		return err
	}
	installed.Store(built)
	return nil
}

// Current returns the installed cipher or ErrKeyMissing.
func Current() (*Cipher, error) {
	if current := installed.Load(); current != nil {
		return current, nil
	}
	return nil, ErrKeyMissing
}

// InstallConfigured installs the key from encodedKey (base64) when set,
// otherwise from keyFile, creating keyFile with a fresh random key (mode
// 0600) when it does not exist. created reports a newly generated file.
func InstallConfigured(encodedKey, keyFile string) (created bool, err error) {
	if encodedKey = strings.TrimSpace(encodedKey); encodedKey != "" {
		key, err := decodeKey(encodedKey)
		if err != nil {
			return false, fmt.Errorf("CREDENTIALS_KEY: %w", err)
		}
		return false, Install(key)
	}
	if strings.TrimSpace(keyFile) == "" {
		return false, errors.New("credential key file path is required")
	}
	content, err := os.ReadFile(keyFile)
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, KeySize)
		if _, err := rand.Read(key); err != nil {
			return false, fmt.Errorf("generate credential key: %w", err)
		}
		if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
			return false, fmt.Errorf("create credential key directory: %w", err)
		}
		// O_EXCL: two processes starting at once never overwrite each other.
		file, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			return InstallConfigured("", keyFile)
		}
		if err != nil {
			return false, fmt.Errorf("create credential key file: %w", err)
		}
		_, writeErr := file.WriteString(base64.StdEncoding.EncodeToString(key) + "\n")
		closeErr := file.Close()
		if writeErr != nil {
			return false, fmt.Errorf("write credential key file: %w", writeErr)
		}
		if closeErr != nil {
			return false, fmt.Errorf("close credential key file: %w", closeErr)
		}
		return true, Install(key)
	}
	if err != nil {
		return false, fmt.Errorf("read credential key file: %w", err)
	}
	key, err := decodeKey(string(content))
	if err != nil {
		return false, fmt.Errorf("credential key file %s: %w", keyFile, err)
	}
	return false, Install(key)
}

func decodeKey(encoded string) ([]byte, error) {
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil || len(key) != KeySize {
		return nil, ErrInvalidKey
	}
	return key, nil
}

// serializer is the GORM credential_cipher serializer: JSON-encode, then
// seal on write; open, then JSON-decode on read.
type serializer struct{}

func init() {
	schema.RegisterSerializer(SerializerName, serializer{})
}

// Scan implements schema.SerializerInterface.
func (serializer) Scan(ctx context.Context, field *schema.Field, destination reflect.Value, databaseValue any) error {
	fieldValue := reflect.New(field.FieldType)
	if databaseValue != nil {
		var text string
		switch value := databaseValue.(type) {
		case string:
			text = value
		case []byte:
			text = string(value)
		default:
			return fmt.Errorf("credential data has unsupported column type %T", databaseValue)
		}
		if text != "" {
			current, err := Current()
			if err != nil {
				return err
			}
			plaintext, err := current.Open(text)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(plaintext, fieldValue.Interface()); err != nil {
				return fmt.Errorf("decode credential data: %w", err)
			}
		}
	}
	field.ReflectValueOf(ctx, destination).Set(fieldValue.Elem())
	return nil
}

// Value implements schema.SerializerInterface.
func (serializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue any) (any, error) {
	plaintext, err := json.Marshal(fieldValue)
	if err != nil {
		return nil, fmt.Errorf("encode credential data: %w", err)
	}
	current, err := Current()
	if err != nil {
		return nil, err
	}
	return current.Seal(plaintext)
}
