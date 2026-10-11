package credential

import (
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// credentialRecordFromModel maps the wire credential onto the credentials
// row. Data stays JSON because the shape is plugin-owned through the
// credential schema; the map passes through verbatim. Order maps onto the
// SortOrder column. Timestamps normalize to UTC.
func credentialRecordFromModel(credential *models.Credential) record.Credential {
	return record.Credential{
		ID:             credential.ID,
		ProviderID:     credential.ProviderID,
		Label:          credential.Label,
		Data:           credential.Data,
		ExpiresAt:      normalizeCredentialTime(credential.ExpiresAt),
		LastUsedAt:     normalizeCredentialTime(credential.LastUsedAt),
		RequestCount:   credential.RequestCount,
		SuccessCount:   credential.SuccessCount,
		FailureCount:   credential.FailureCount,
		Disabled:       credential.Disabled,
		DisabledBy:     credential.DisabledBy,
		DisabledReason: credential.DisabledReason,
		DisabledAt:     normalizeCredentialTime(credential.DisabledAt),
		SortOrder:      credential.Order,
		CreatedAt:      credential.CreatedAt.UTC(),
		UpdatedAt:      credential.UpdatedAt.UTC(),
	}
}

// credentialModelFromRecord rebuilds the wire credential from the
// credentials row. The JSON data map returns verbatim; SortOrder returns as
// Order. Timestamps return in UTC.
func credentialModelFromRecord(row record.Credential) *models.Credential {
	return &models.Credential{
		ID:             row.ID,
		ProviderID:     row.ProviderID,
		Label:          row.Label,
		Data:           row.Data,
		ExpiresAt:      normalizeCredentialTime(row.ExpiresAt),
		CreatedAt:      row.CreatedAt.UTC(),
		UpdatedAt:      row.UpdatedAt.UTC(),
		LastUsedAt:     normalizeCredentialTime(row.LastUsedAt),
		RequestCount:   row.RequestCount,
		SuccessCount:   row.SuccessCount,
		FailureCount:   row.FailureCount,
		Disabled:       row.Disabled,
		DisabledBy:     row.DisabledBy,
		DisabledReason: row.DisabledReason,
		DisabledAt:     normalizeCredentialTime(row.DisabledAt),
		Order:          row.SortOrder,
	}
}

// parkRecordFromModel maps the wire park entry onto the credential_parks
// row. The credential reference stays a hard foreign key with cascade
// delete. Timestamps normalize to UTC.
func parkRecordFromModel(entry *models.ParkEntry) record.CredentialPark {
	return record.CredentialPark{
		CredentialID: entry.CredentialID,
		Reason:       entry.Reason,
		Until:        entry.Until.UTC(),
		ParkedAt:     entry.ParkedAt.UTC(),
	}
}

// parkModelFromRecord rebuilds the wire park entry from the credential_parks
// row. Timestamps return in UTC.
func parkModelFromRecord(row record.CredentialPark) *models.ParkEntry {
	return &models.ParkEntry{
		CredentialID: row.CredentialID,
		Reason:       row.Reason,
		Until:        row.Until.UTC(),
		ParkedAt:     row.ParkedAt.UTC(),
	}
}

// normalizeCredentialTime copies an optional timestamp into UTC. A missing
// value stays missing.
func normalizeCredentialTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := value.UTC()
	return &copied
}
