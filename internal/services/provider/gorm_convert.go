package provider

import (
	"strings"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/db/record"
	"github.com/TheSlopMachine/llm-router/internal/models"
)

// Reserved provider configuration keys owned by the router. Split strips
// them from settings; compose restores them from columns. Plugin keys pass
// through settings verbatim.
const (
	providerConfigBaseURLKey       = "base_url"
	providerConfigProxyKey         = "proxy"
	providerConfigProxyPoolKey     = "pool"
	providerConfigDisableFailedKey = "disable_failed_credentials"
)

// splitProviderConfig divides a dashboard configuration map into router
// columns and plugin settings. BaseURL copies when a non-empty string
// reads present; proxy pool copies when proxy.pool trims non-empty;
// disable_failed_credentials reads false for absent or non-boolean,
// matching CredentialAutomationOn. All other keys copy to settings.
// The input map stays unmodified.
func splitProviderConfig(configuration map[string]any) (baseURL *string, proxyPoolID *string, disableFailed bool, settings map[string]any) {
	settings = make(map[string]any, len(configuration))
	if len(configuration) == 0 {
		return nil, nil, false, settings
	}
	if raw, ok := configuration[providerConfigBaseURLKey].(string); ok && strings.TrimSpace(raw) != "" {
		copied := raw
		baseURL = &copied
	}
	if rawProxy, ok := configuration[providerConfigProxyKey].(map[string]any); ok {
		if pool, ok := rawProxy[providerConfigProxyPoolKey].(string); ok && strings.TrimSpace(pool) != "" {
			trimmed := strings.TrimSpace(pool)
			proxyPoolID = &trimmed
		}
	}
	if enabled, ok := configuration[providerConfigDisableFailedKey].(bool); ok {
		disableFailed = enabled
	}
	for key, value := range configuration {
		if key == providerConfigBaseURLKey || key == providerConfigProxyKey || key == providerConfigDisableFailedKey {
			continue
		}
		settings[key] = value
	}
	return baseURL, proxyPoolID, disableFailed, settings
}

// composeProviderConfig builds the Lua-visible provider configuration from
// columns and settings. BaseURL appears at the top level when set;
// proxy.pool appears only when a pool reads set; disable_failed_credentials
// always appears with its stored value; remaining keys come from settings.
// The result is a fresh map on every call.
func composeProviderConfig(baseURL *string, proxyPoolID *string, disableFailed bool, settings map[string]any) map[string]any {
	configuration := make(map[string]any, len(settings)+3)
	for key, value := range settings {
		configuration[key] = value
	}
	if baseURL != nil && *baseURL != "" {
		configuration[providerConfigBaseURLKey] = *baseURL
	}
	if proxyPoolID != nil && strings.TrimSpace(*proxyPoolID) != "" {
		configuration[providerConfigProxyKey] = map[string]any{providerConfigProxyPoolKey: *proxyPoolID}
	}
	configuration[providerConfigDisableFailedKey] = disableFailed
	return configuration
}

// qualifierFromID derives the qualifier suffix from a provider identifier.
// Custom rows never carry a qualifier; the singleton slot (ID equal to the
// type key) reads empty; qualified rows return the suffix after the type
// prefix. The schema drops the qualifier column, so reads return the
// slugified suffix rather than the raw creation value. Dedup-suffixed
// duplicates (type:qual-2) return the suffixed form.
func qualifierFromID(identifier, typeKey string) string {
	if typeKey == TypeCustom {
		return ""
	}
	if identifier == typeKey {
		return ""
	}
	prefix := typeKey + ":"
	if strings.HasPrefix(identifier, prefix) {
		return strings.TrimPrefix(identifier, prefix)
	}
	return ""
}

// providerRecordFromModel maps the wire instance onto the provider_instances
// row. Configuration splits into columns and settings; the qualifier stays
// out of storage and derives from the identifier on reads. Timestamps
// normalize to UTC.
func providerRecordFromModel(instance *models.ProviderInstance) record.ProviderInstance {
	baseURL, proxyPoolID, disableFailed, settings := splitProviderConfig(instance.Config)
	return record.ProviderInstance{
		ID:                       instance.ID,
		Name:                     instance.Name,
		TypeKey:                  instance.TypeKey,
		BaseURL:                  baseURL,
		ProxyPoolID:              proxyPoolID,
		DisableFailedCredentials: disableFailed,
		Disabled:                 instance.Disabled,
		DisabledBy:               instance.DisabledBy,
		DisabledReason:           instance.DisabledReason,
		DisabledAt:               normalizeProviderTime(instance.DisabledAt),
		IsUIReadonly:             instance.IsUIReadonly,
		IsUIHidden:               instance.IsUIHidden,
		IconURL:                  instance.IconURL,
		Settings:                 settings,
		CreatedAt:                instance.CreatedAt.UTC(),
		UpdatedAt:                instance.UpdatedAt.UTC(),
	}
}

// providerModelFromRecord rebuilds the wire instance from the
// provider_instances row. Configuration composes from columns and settings;
// the qualifier derives from the identifier. Timestamps return in UTC.
func providerModelFromRecord(row record.ProviderInstance) *models.ProviderInstance {
	return &models.ProviderInstance{
		ID:             row.ID,
		Name:           row.Name,
		TypeKey:        row.TypeKey,
		Qualifier:      qualifierFromID(row.ID, row.TypeKey),
		Config:         composeProviderConfig(row.BaseURL, row.ProxyPoolID, row.DisableFailedCredentials, row.Settings),
		IconURL:        row.IconURL,
		IsUIReadonly:   row.IsUIReadonly,
		IsUIHidden:     row.IsUIHidden,
		Disabled:       row.Disabled,
		DisabledBy:     row.DisabledBy,
		DisabledReason: row.DisabledReason,
		DisabledAt:     normalizeProviderTime(row.DisabledAt),
		CreatedAt:      row.CreatedAt.UTC(),
		UpdatedAt:      row.UpdatedAt.UTC(),
	}
}

// normalizeProviderTime copies an optional timestamp into UTC. A missing
// value stays missing.
func normalizeProviderTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copied := value.UTC()
	return &copied
}
