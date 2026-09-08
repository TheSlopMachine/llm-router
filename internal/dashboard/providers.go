package dashboard

import (
	"encoding/json"
	"net/http"

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
}

func toProviderView(p *models.ProviderInstance, svc *provider.Service) providerView {
	config := p.Config
	if config == nil {
		config = map[string]any{}
	}
	baseURL, _ := config["base_url"].(string)
	return providerView{
		ID: p.ID, Name: p.Name, TypeKey: p.TypeKey, Type: p.TypeKey,
		Qualifier: p.Qualifier, Config: config, BaseURL: baseURL, IconURL: p.IconURL,
		SupportsAuthFlow: svc.SupportsAuthFlow(p.TypeKey),
		IsUIReadonly:     p.IsUIReadonly, IsUIHidden: p.IsUIHidden,
	}
}

// visibleProviders drops UI-hidden rows. Router, models and metrics keep
// using the full service-level list.
func visibleProviders(all []*models.ProviderInstance) []*models.ProviderInstance {
	out := make([]*models.ProviderInstance, 0, len(all))
	for _, p := range all {
		if p.IsUIHidden {
			continue
		}
		out = append(out, p)
	}
	return out
}

// loadVisibleProvider resolves a provider for UI management endpoints.
// Hidden rows behave as nonexistent.
func (h *Handler) loadVisibleProvider(id string) (*models.ProviderInstance, bool) {
	p, err := h.providerSvc.Get(id)
	if err != nil || p.IsUIHidden {
		return nil, false
	}
	return p, true
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
	providers, err := h.providerSvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	providers = visibleProviders(providers)
	out := make([]providerView, 0, len(providers))
	for _, p := range providers {
		out = append(out, toProviderView(p, h.providerSvc))
	}
	h.json(w, http.StatusOK, out)
}

// apiAdapterTypes lists available provider type keys (Go backends + Lua plugins).
// @Summary      List provider types
// @Description  Returns all registered provider type keys with UI creation flags.
// @Tags         Providers
// @Produce      json
// @Success      200 {array} object{type_key=string,creatable=bool}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/adapter-types [get]
func (h *Handler) apiAdapterTypes(w http.ResponseWriter, r *http.Request) {
	keys := h.providerSvc.TypeKeys()
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{"type_key": k, "creatable": provider.IsCreatableTypeKey(k)})
	}
	h.json(w, http.StatusOK, out)
}

// apiProvidersStats returns aggregated statistics for all providers
// @Summary      Get provider statistics
// @Description  Returns aggregated statistics for all providers (model count, credential count, requests in last 24h)
// @Tags         Providers
// @Produce      json
// @Success      200 {object} map[string]models.ProviderStats
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/stats [get]
func (h *Handler) apiProvidersStats(w http.ResponseWriter, r *http.Request) {
	providers, err := h.providerSvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	providers = visibleProviders(providers)

	stats := make(map[string]*models.ProviderStats)
	ctx := r.Context()

	for _, p := range providers {
		stat := &models.ProviderStats{}

		modelInfos, err := h.modelInfoSvc.GetModelInfos(ctx, p.ID)
		if err == nil {
			stat.ModelCount = len(modelInfos)
		}

		creds, err := h.credSvc.ListByProvider(p.ID)
		if err == nil {
			stat.CredentialCount = len(creds)
		}

		filters := models.MetricsFilters{
			ProviderID: p.ID,
			TimeRange:  "1d",
		}
		overview, err := h.metricsSvc.QueryOverview(filters)
		if err == nil && overview != nil {
			stat.RequestsToday = overview.TotalRequests
		}

		stats[p.ID] = stat
	}

	h.json(w, http.StatusOK, stats)
}

// apiProvidersCreate creates a new provider instance for any type.
// @Summary      Create provider
// @Description  Creates a new provider instance for any registered type.
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

	inst, err := h.providerSvc.Create(provider.CreateOptions{
		Name: body.Name, TypeKey: body.TypeKey, Qualifier: body.Qualifier,
		Config: body.Config, IconURL: body.IconURL,
	})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	h.json(w, http.StatusOK, toProviderView(inst, h.providerSvc))
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
	if existing.IsUIReadonly {
		h.jsonErr(w, http.StatusForbidden, "provider is managed automatically")
		return
	}

	var body models.ProviderInstanceUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
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

	inst, err := h.providerSvc.Update(id, provider.UpdateOptions{
		Name: body.Name, Config: body.Config, IconURL: body.IconURL,
	})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}

	h.json(w, http.StatusOK, toProviderView(inst, h.providerSvc))
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

	if err := h.providerSvc.Delete(id); err != nil {
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
	p, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	nodes, err := h.providerSvc.ConfigSchema(p.TypeKey)
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

// apiTypeSchemas returns config or credential UI trees for a type key.
// Used when creating a provider before any instance exists.
// @Summary      Get type schemas
// @Description  Returns config or credential UI trees for a provider type key.
// @Tags         Providers
// @Produce      json
// @Param        kind query string true "Schema kind: config or credential"
// @Param        type_key query string true "Provider type key"
// @Success      200 {object} object{nodes=[]models.UINode,fallback=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/type-schemas [get]
func (h *Handler) apiTypeSchemas(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	typeKey := r.URL.Query().Get("type_key")
	if typeKey == "" {
		h.jsonErr(w, http.StatusBadRequest, "type_key is required")
		return
	}
	var nodes []models.UINode
	var err error
	switch kind {
	case "config":
		var n []*models.UINode
		n, err = h.providerSvc.ConfigSchema(typeKey)
		for _, node := range n {
			nodes = append(nodes, *node)
		}
	case "credential":
		var n []*models.UINode
		n, err = h.providerSvc.CredentialSchema(typeKey)
		for _, node := range n {
			nodes = append(nodes, *node)
		}
	default:
		h.jsonErr(w, http.StatusBadRequest, "kind must be config or credential")
		return
	}
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
	p, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	nodes, err := h.providerSvc.CredentialSchema(p.TypeKey)
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
