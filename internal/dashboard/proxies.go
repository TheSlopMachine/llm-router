package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// apiProxiesList returns the full proxy pool
// @Summary      List proxies
// @Description  Returns all pooled proxies, manual first, with health info.
// @Tags         Proxies
// @Produce      json
// @Success      200 {array} models.Proxy
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxies [get]
func (h *Handler) apiProxiesList(w http.ResponseWriter, r *http.Request) {
	all, err := h.proxySvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if all == nil {
		all = []*models.Proxy{}
	}
	h.json(w, http.StatusOK, all)
}

// apiProxiesAdd registers a manual proxy
// @Summary      Add proxy
// @Description  Registers a user-provided proxy (http/https/socks4/socks5, optional auth).
// @Tags         Proxies
// @Accept       json
// @Produce      json
// @Param        body body object{url=string,country=string} true "Proxy URL"
// @Success      200 {object} models.Proxy
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxies [post]
func (h *Handler) apiProxiesAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL     string `json:"url"`
		Country string `json:"country"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.URL == "" {
		h.jsonErr(w, http.StatusBadRequest, "url is required")
		return
	}
	p, err := h.proxySvc.AddManual(body.URL, body.Country)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, p)
}

// apiProxiesDelete removes a proxy
// @Summary      Delete proxy
// @Tags         Proxies
// @Param        id path string true "Proxy ID"
// @Success      204 "No Content"
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxies/{id} [delete]
func (h *Handler) apiProxiesDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.proxySvc.Delete(r.PathValue("id")); err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiProxiesCheck probes one proxy
// @Summary      Check proxy
// @Description  Runs a health probe and culls the proxy when dead.
// @Tags         Proxies
// @Produce      json
// @Param        id path string true "Proxy ID"
// @Success      200 {object} object{alive=bool,latency_ms=int,error=string}
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxies/{id}/check [post]
func (h *Handler) apiProxiesCheck(w http.ResponseWriter, r *http.Request) {
	p, err := h.proxySvc.Check(r.Context(), r.PathValue("id"))
	out := map[string]any{"alive": err == nil && p != nil && p.Alive}
	if p != nil {
		out["latency_ms"] = p.LatencyMs
	}
	if err != nil {
		out["error"] = err.Error()
	}
	h.json(w, http.StatusOK, out)
}

// apiProxiesCheckAll probes the whole pool
// @Summary      Check all proxies
// @Tags         Proxies
// @Success      204 "No Content"
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxies/check-all [post]
func (h *Handler) apiProxiesCheckAll(w http.ResponseWriter, r *http.Request) {
	go h.proxySvc.CheckAll(r.Context())
	w.WriteHeader(http.StatusNoContent)
}

// apiProxySources lists registered proxy-list source plugins
// @Summary      List proxy sources
// @Tags         Proxies
// @Produce      json
// @Success      200 {array} string
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy-sources [get]
func (h *Handler) apiProxySources(w http.ResponseWriter, r *http.Request) {
	keys := h.luaSvc.ProxySourceKeys()
	if keys == nil {
		keys = []string{}
	}
	h.json(w, http.StatusOK, keys)
}

// apiProxySourceRefresh fetches a fresh proxy list from a source plugin
// @Summary      Refresh proxy source
// @Description  Invokes the source plugin's fetch_proxies and syncs the pool.
// @Tags         Proxies
// @Produce      json
// @Param        key path string true "Source type key"
// @Success      200 {object} object{added=int}
// @Failure      401 {object} models.ErrorResponse
// @Failure      502 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy-sources/{key}/refresh [post]
func (h *Handler) apiProxySourceRefresh(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	candidates, err := h.luaSvc.FetchProxies(r.Context(), key)
	if err != nil {
		h.jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	added, err := h.proxySvc.SyncFromSource(key, candidates)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{"added": added, "total": len(candidates)})
}

// apiProxyStatus reports server location and pool stats
// @Summary      Proxy status
// @Produce      json
// @Success      200 {object} object{server_country=string,total=int,alive=int}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy/status [get]
func (h *Handler) apiProxyStatus(w http.ResponseWriter, r *http.Request) {
	all, _ := h.proxySvc.List()
	alive := 0
	for _, p := range all {
		if p.Alive {
			alive++
		}
	}
	h.json(w, http.StatusOK, map[string]any{
		"server_country": h.geoSvc.Country(r.Context()),
		"total":          len(all),
		"alive":          alive,
	})
}
