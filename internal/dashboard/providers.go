package dashboard

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/provider"
)

type providerView struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	TypeKey          string         `json:"type_key"`
	Type             string         `json:"type"`
	Qualifier        string         `json:"qualifier"`
	Config           map[string]any `json:"config"`
	BaseURL          string         `json:"base_url"`
	IconURL          string         `json:"icon_url"`
	SupportsAuthFlow bool           `json:"supports_auth_flow"`
	IsUIReadonly     bool           `json:"is_ui_readonly"`
	IsUIHidden       bool           `json:"is_ui_hidden"`
	Disabled         bool           `json:"disabled"`
	DisabledBy       string         `json:"disabled_by,omitempty"`
	DisabledReason   string         `json:"disabled_reason,omitempty"`
	DisabledAt       *time.Time     `json:"disabled_at,omitempty"`
}

func toProviderView(provider *models.ProviderInstance, providerService *provider.Service) providerView {
	config := provider.Config
	if config == nil {
		config = map[string]any{}
	}
	baseURL, _ := config["base_url"].(string)
	return providerView{
		ID: provider.ID, Name: provider.Name, TypeKey: provider.TypeKey, Type: provider.TypeKey,
		Qualifier: provider.Qualifier, Config: config, BaseURL: baseURL, IconURL: provider.IconURL,
		SupportsAuthFlow: providerService.SupportsAuthFlow(provider.TypeKey),
		IsUIReadonly:     provider.IsUIReadonly, IsUIHidden: provider.IsUIHidden,
		Disabled: provider.Disabled, DisabledBy: provider.DisabledBy, DisabledReason: provider.DisabledReason,
		DisabledAt: provider.DisabledAt,
	}
}

// providerTypeKnown reports whether the type key can actually serve requests:
// built-in Go backends always; Lua types only while their plugin is installed.
// A nil Lua service means the subsystem is absent, not that nothing is installed.
func (h *Handler) providerTypeKnown(typeKey string) bool {
	if typeKey == provider.TypeCustom || typeKey == provider.TypeVirtual {
		return true
	}
	if h.luaService == nil {
		return true
	}
	_, err := h.luaService.Lookup(typeKey)
	return err == nil
}

// visibleProviders drops UI-hidden rows and rows whose plugin is uninstalled.
// Router, models and metrics keep using the full service-level list.
func (h *Handler) visibleProviders(all []*models.ProviderInstance) []*models.ProviderInstance {
	out := make([]*models.ProviderInstance, 0, len(all))
	for _, provider := range all {
		if provider.IsUIHidden || !h.providerTypeKnown(provider.TypeKey) {
			continue
		}
		if h.providerService != nil && !h.providerService.IsTypeAvailable(provider.TypeKey) {
			continue
		}
		out = append(out, provider)
	}
	return out
}

// loadVisibleProvider resolves a provider for UI management endpoints.
// Hidden rows and rows with an uninstalled plugin behave as nonexistent.
func (h *Handler) loadVisibleProvider(id string) (*models.ProviderInstance, bool) {
	provider, err := h.providerService.Get(id)
	if err != nil || provider.IsUIHidden || !h.providerTypeKnown(provider.TypeKey) {
		return nil, false
	}
	if !h.providerService.IsTypeAvailable(provider.TypeKey) {
		return nil, false
	}
	return provider, true
}

// apiProvidersList lists all providers
// @Summary      List providers
// @Description  Returns all configured provider instances.
// @Tags         Providers
// @Produce      json
// @Success      200 {array} object{id=string,name=string,type_key=string,type=string,qualifier=string,config=object,base_url=string,icon_url=string,supports_auth_flow=bool}
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers [get]
func (h *Handler) apiProvidersList(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerService.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	providers = h.visibleProviders(providers)
	out := make([]providerView, 0, len(providers))
	for _, provider := range providers {
		out = append(out, toProviderView(provider, h.providerService))
	}
	h.json(w, http.StatusOK, out)
}

// apiProvidersStats returns aggregated statistics for all providers
// @Summary      Get provider statistics
// @Description  Returns aggregated statistics for all providers (cached model count, credential count). Never fetches from upstreams.
// @Tags         Providers
// @Produce      json
// @Success      200 {object} map[string]models.ProviderStats
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/stats [get]
func (h *Handler) apiProvidersStats(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerService.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	providers = h.visibleProviders(providers)

	statistics := make(map[string]*models.ProviderStats)

	for _, provider := range providers {
		entry := &models.ProviderStats{}

		// Cache peek only: a stats read must never fetch from the upstream.
		entry.ModelCount = len(h.modelInfoService.PeekModelInfos(provider.ID))

		credentials, err := h.credentialService.ListByProvider(provider.ID)
		if err == nil {
			entry.CredentialCount = len(credentials)
		}

		statistics[provider.ID] = entry
	}

	h.json(w, http.StatusOK, statistics)
}

// apiProvidersCreate creates a new custom (OpenAI-compatible) provider instance.
// Plugin types are registered by plugins and seeded by the core; users
// cannot create rows for other types through this endpoint.
// @Summary      Create provider
// @Description  Creates a new custom OpenAI-compatible provider instance.
// @Tags         Providers
// @Accept       json
// @Produce      json
// @Param        body body models.ProviderInstanceCreateRequest true "Provider details"
// @Success      200 {object} models.ProviderInstance
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers [post]
func (h *Handler) apiProvidersCreate(w http.ResponseWriter, r *http.Request) {
	var body models.ProviderInstanceCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Backward-compatible custom shape: {name, base_url, icon_url}.
	if body.TypeKey == "" {
		var legacy struct {
			Name    string `json:"name"`
			BaseURL string `json:"base_url"`
			IconURL string `json:"icon_url"`
		}
		raw, _ := json.Marshal(body)
		if err := json.Unmarshal(raw, &legacy); err == nil && legacy.BaseURL != "" {
			body.TypeKey = provider.TypeCustom
			body.Config = map[string]any{"base_url": legacy.BaseURL}
			if body.Name == "" {
				body.Name = legacy.Name
			}
			if body.IconURL == "" {
				body.IconURL = legacy.IconURL
			}
		}
	}
	if body.TypeKey != provider.TypeCustom {
		h.jsonErr(w, http.StatusBadRequest, "only custom providers can be created")
		return
	}

	instance, err := h.providerService.Create(provider.CreateOptions{
		Name: body.Name, TypeKey: provider.TypeCustom,
		Config: body.Config, IconURL: body.IconURL,
	})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	h.json(w, http.StatusOK, toProviderView(instance, h.providerService))
}

// apiProvidersUpdate updates an existing provider instance.
// @Summary      Update provider
// @Description  Updates a provider instance's details.
// @Tags         Providers
// @Accept       json
// @Produce      json
// @Param        id path string true "Provider ID"
// @Param        body body models.ProviderInstanceUpdateRequest true "Provider details"
// @Success      200 {object} models.ProviderInstance
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id} [put]
func (h *Handler) apiProvidersUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "id is required")
		return
	}

	existing, ok := h.loadVisibleProvider(id)
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}

	var body models.ProviderInstanceUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if existing.IsUIReadonly {
		// Seeded providers are managed automatically; only operational keys
		// (proxy pool selection, model automation) and the disabled
		// toggle may be edited. Other config keys are preserved.
		allowed := map[string]bool{"proxy": true, "models_auto_sync": true, "disable_failed_models": true, "disable_failed_credentials": true}
		if body.Name != "" && body.Name != existing.Name {
			h.jsonErr(w, http.StatusForbidden, "provider is managed automatically")
			return
		}
		for key := range body.Config {
			if !allowed[key] {
				h.jsonErr(w, http.StatusForbidden, "provider is managed automatically")
				return
			}
		}
		mergedConfig := map[string]any{}
		for key, value := range existing.Config {
			mergedConfig[key] = value
		}
		// Absent or null allowed keys are removals, not untouched state:
		// the dashboard sends the full config, so a missing key means the
		// operator cleared it. Preserving it instead makes disables
		// impossible to persist.
		for key := range allowed {
			if value, ok := body.Config[key]; ok && value != nil {
				mergedConfig[key] = value
			} else {
				delete(mergedConfig, key)
			}
		}
		instance, err := h.providerService.Update(id, provider.UpdateOptions{Name: existing.Name, Config: mergedConfig, IconURL: existing.IconURL, Disabled: body.Disabled})
		if err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if body.Disabled != nil {
			h.logger.Info("provider admin state change", "provider_id", id, "disabled", *body.Disabled)
		}
		h.json(w, http.StatusOK, toProviderView(instance, h.providerService))
		return
	}

	// Backward-compatible custom shape.
	if body.Config == nil {
		var legacy struct {
			Name    string `json:"name"`
			BaseURL string `json:"base_url"`
			IconURL string `json:"icon_url"`
		}
		raw, _ := json.Marshal(body)
		if err := json.Unmarshal(raw, &legacy); err == nil && legacy.BaseURL != "" {
			body.Config = map[string]any{"base_url": legacy.BaseURL}
			if body.Name == "" {
				body.Name = legacy.Name
			}
			if body.IconURL == "" {
				body.IconURL = legacy.IconURL
			}
		}
	}

	instance, err := h.providerService.Update(id, provider.UpdateOptions{
		Name: body.Name, Config: body.Config, IconURL: body.IconURL, Disabled: body.Disabled,
	})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Disabled != nil {
		h.logger.Info("provider admin state change", "provider_id", id, "disabled", *body.Disabled)
	}

	h.json(w, http.StatusOK, toProviderView(instance, h.providerService))
}

// apiProvidersDelete deletes a provider and cascades its credentials.
// @Summary      Delete provider
// @Description  Deletes a provider instance.
// @Tags         Providers
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {object} object{message=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id} [delete]
func (h *Handler) apiProvidersDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "id is required")
		return
	}

	existing, ok := h.loadVisibleProvider(id)
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	if existing.IsUIReadonly {
		h.jsonErr(w, http.StatusForbidden, "provider is managed automatically")
		return
	}

	if err := h.providerService.Delete(id); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	h.json(w, http.StatusOK, map[string]string{"message": "provider deleted"})
}

// apiProviderConfigSchema returns the config UI tree for a provider.
// A null body means the dashboard falls back to raw JSON input.
// @Summary      Get provider config schema
// @Description  Returns the config UI tree for a provider instance.
// @Tags         Providers
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {object} object{nodes=[]models.UINode,fallback=string}
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/config-schema [get]
func (h *Handler) apiProviderConfigSchema(w http.ResponseWriter, r *http.Request) {
	provider, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	nodes, err := h.providerService.ConfigSchema(provider.TypeKey)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if nodes == nil {
		h.json(w, http.StatusOK, map[string]any{"nodes": nil, "fallback": "raw_json"})
		return
	}
	h.json(w, http.StatusOK, map[string]any{"nodes": nodes})
}

// apiProviderCredentialSchema returns the credential UI tree for a provider.
// @Summary      Get provider credential schema
// @Description  Returns the credential UI tree for a provider instance.
// @Tags         Providers
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {object} object{nodes=[]models.UINode,fallback=string}
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/credential-schema [get]
func (h *Handler) apiProviderCredentialSchema(w http.ResponseWriter, r *http.Request) {
	provider, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	nodes, err := h.providerService.CredentialSchema(provider.TypeKey)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if nodes == nil {
		h.json(w, http.StatusOK, map[string]any{
			"nodes":   nil,
			"enabled": h.providerService.CredentialsEnabled(provider.TypeKey),
		})
		return
	}
	h.json(w, http.StatusOK, map[string]any{"nodes": nodes, "enabled": true})
}

// apiProviderSettingsSchema returns the plugin settings UI tree.
func (h *Handler) apiProviderSettingsSchema(w http.ResponseWriter, r *http.Request) {
	provider, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	nodes, err := h.providerService.SettingsSchema(provider.TypeKey)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if nodes == nil {
		h.json(w, http.StatusOK, map[string]any{"nodes": nil})
		return
	}
	h.json(w, http.StatusOK, map[string]any{"nodes": nodes})
}

// apiProviderProxySchema returns the extra proxy settings UI tree plus the
// capability flag driving the proxy tab.
func (h *Handler) apiProviderProxySchema(w http.ResponseWriter, r *http.Request) {
	provider, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	nodes, err := h.providerService.ProxySchema(provider.TypeKey)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{
		"nodes":   nodes,
		"enabled": h.providerService.ProxiesEnabled(provider.TypeKey),
	})
}

// apiProviderJobRun triggers one plugin job for a provider on demand.
// @Summary      Run provider job
// @Description  Triggers one colocated plugin job for a provider instance.
// @Tags         Providers
// @Param        id path string true "Provider ID"
// @Param        job path string true "Job name"
// @Success      200 {object} object{ok=bool}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/jobs/{job} [post]
func (h *Handler) apiProviderJobRun(w http.ResponseWriter, r *http.Request) {
	provider, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	if h.maintenanceService == nil {
		h.jsonErr(w, http.StatusInternalServerError, "job scheduler not wired")
		return
	}
	if err := h.maintenanceService.RunJob(r.Context(), provider.ID, r.PathValue("job")); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}
