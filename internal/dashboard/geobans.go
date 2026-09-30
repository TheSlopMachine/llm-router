package dashboard

import (
	"net/http"
	"net/url"
)

type geoBanView struct {
	ProxyID   string `json:"proxy_id"`
	ProxyHost string `json:"proxy_host,omitempty"`
	Region    string `json:"region,omitempty"`
	BannedAt  string `json:"banned_at"`
	Reason    string `json:"reason,omitempty"`
}

// apiGeoBansList lists indefinite geo flags of one provider.
// @Summary      List geo bans
// @Description  Returns indefinite (provider, proxy) geo-block flags with proxy details.
// @Tags         Providers
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {object} object{bans=[]object{proxy_id=string,proxy_host=string,region=string,banned_at=string,reason=string}}
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/geo-bans [get]
func (h *Handler) apiGeoBansList(w http.ResponseWriter, r *http.Request) {
	if h.geobanSvc == nil {
		h.json(w, http.StatusOK, map[string]any{"bans": []geoBanView{}})
		return
	}
	p, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	rec, err := h.luaSvc.Lookup(p.TypeKey)
	if err != nil {
		h.json(w, http.StatusOK, map[string]any{"bans": []geoBanView{}})
		return
	}
	bans, err := h.geobanSvc.ListProvider(rec.ID, p.TypeKey)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]geoBanView, 0, len(bans))
	for _, b := range bans {
		v := geoBanView{
			ProxyID:  b.Proxy,
			BannedAt: b.BannedAt.Format("2006-01-02T15:04:05Z07:00"),
			Reason:   b.Reason,
		}
		if h.proxySvc != nil {
			if px, err := h.proxySvc.Get(b.Proxy); err == nil && px != nil {
				proxyURL, err := url.Parse(px.URL)
				if err != nil {
					h.jsonErr(w, http.StatusInternalServerError, "invalid cached proxy URL")
					return
				}
				v.ProxyHost = proxyURL.Host
				v.Region = px.Location
			}
		}
		out = append(out, v)
	}
	h.json(w, http.StatusOK, map[string]any{"bans": out})
}

// apiGeoBansClearAll clears every geo flag of one provider.
// @Summary      Clear geo bans
// @Description  Removes all indefinite geo-block flags of a provider.
// @Tags         Providers
// @Param        id path string true "Provider ID"
// @Success      204 "No Content"
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/geo-bans [delete]
func (h *Handler) apiGeoBansClearAll(w http.ResponseWriter, r *http.Request) {
	if h.geobanSvc == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	p, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	rec, err := h.luaSvc.Lookup(p.TypeKey)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	removed, err := h.geobanSvc.ClearProvider(rec.ID, p.TypeKey)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.logger.Info("geo bans cleared by admin", "provider_id", r.PathValue("id"), "count", removed)
	w.WriteHeader(http.StatusNoContent)
}

// apiGeoBansClearOne clears one geo flag of a provider.
// @Summary      Clear one geo ban
// @Description  Removes the indefinite geo-block flag of one proxy.
// @Tags         Providers
// @Param        id path string true "Provider ID"
// @Param        proxyId path string true "Proxy ID"
// @Success      204 "No Content"
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/geo-bans/{proxyId} [delete]
func (h *Handler) apiGeoBansClearOne(w http.ResponseWriter, r *http.Request) {
	if h.geobanSvc == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	p, ok := h.loadVisibleProvider(r.PathValue("id"))
	if !ok {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	proxyID := r.PathValue("proxyId")
	if proxyID == "" {
		h.jsonErr(w, http.StatusBadRequest, "proxy id is required")
		return
	}
	rec, err := h.luaSvc.Lookup(p.TypeKey)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	banned, err := h.geobanSvc.IsBanned(rec.ID, p.TypeKey, proxyID)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !banned {
		h.jsonErr(w, http.StatusNotFound, "geo ban not found")
		return
	}
	if err := h.geobanSvc.Clear(rec.ID, p.TypeKey, proxyID); err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.logger.Info("geo ban cleared by admin", "provider_id", r.PathValue("id"), "proxy_id", proxyID)
	w.WriteHeader(http.StatusNoContent)
}
