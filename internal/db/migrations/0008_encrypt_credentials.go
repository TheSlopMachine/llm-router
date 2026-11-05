// Encrypt credentials owns migration version 7 of the durable database.
//
// Version 7 seals every plaintext credentials.data value with the installed
// credential key (AES-256-GCM, credentialcipher). From this version on the
// record serializer reads sealed values only, so the migration must run
// before any credential read. A missing key fails the migration and with it
// startup: plaintext never survives silently.
package migrations

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/TheSlopMachine/llm-router/internal/credentialcipher"
)

// credentialDataSnapshot reads and writes the raw data column as text.
type credentialDataSnapshot struct {
	ID   string  `gorm:"primaryKey"`
	Data *string `gorm:"column:data"`
}

// TableName pins the credentials table name.
func (credentialDataSnapshot) TableName() string {
	return "credentials"
}

func upEncryptCredentials(transaction *gorm.DB) error {
	ctx := context.Background()
	rows, err := gorm.G[credentialDataSnapshot](transaction).Find(ctx)
	if err != nil {
		return fmt.Errorf("read credentials: %w", err)
	}
	var cipher *credentialcipher.Cipher
	for _, row := range rows {
		if row.Data == nil || *row.Data == "" || strings.HasPrefix(*row.Data, credentialcipher.SealedPrefix) {
			continue
		}
		if cipher == nil {
			cipher, err = credentialcipher.Current()
			if err != nil {
				return fmt.Errorf("encrypt credentials: %w", err)
			}
		}
		sealed, err := cipher.Seal([]byte(*row.Data))
		if err != nil {
			return fmt.Errorf("seal credential %s: %w", row.ID, err)
		}
		if _, err := gorm.G[credentialDataSnapshot](transaction, clause.Eq{Column: "id", Value: row.ID}).Update(ctx, "data", sealed); err != nil {
			return fmt.Errorf("store sealed credential %s: %w", row.ID, err)
		}
	}
	return nil
}
