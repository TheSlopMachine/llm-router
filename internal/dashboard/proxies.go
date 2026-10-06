package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

// apiProxiesList returns proxies accepted by the library health checks.
// @Summary      List proxies
// @Description  Returns proxies that passed the proxypool health checks.
// @Tags         Proxies
// @Produce      json
// @Success      200 {array} proxypool.Proxy
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxies [get]
func (h *Handler) apiProxiesList(w http.ResponseWriter, _ *http.Request) {
	all, err := h.proxySvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if all == nil {
		all = []*proxypool.Proxy{}
	}
	h.json(w, http.StatusOK, all)
}

// apiProxyRefresh requests an immediate source ingest.
// @Summary      Refresh proxy pool
// @Description  Requests an immediate ingest of all registered proxy sources.
// @Tags         Proxies
// @Produce      json
// @Success      202 {object} object{started=bool,reason=string}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxies/refresh [post]
func (h *Handler) apiProxyRefresh(w http.ResponseWriter, _ *http.Request) {
	started := h.proxySvc.RequestIngest()
	response := map[string]any{"started": started}
	if !started {
		response["reason"] = "proxy pool is not running"
	}
	h.json(w, http.StatusAccepted, response)
}

// apiProxySources reports fetch diagnostics for registered proxy sources.
// @Summary      List proxy sources
// @Description  Returns fetch diagnostics for registered proxy-source plugins.
// @Tags         Proxies
// @Produce      json
// @Success      200 {array} proxypool.SourceInfo
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy-sources [get]
func (h *Handler) apiProxySources(w http.ResponseWriter, _ *http.Request) {
	sources := h.proxySvc.SourceInfos()
	if sources == nil {
		sources = []proxypool.SourceInfo{}
	}
	h.json(w, http.StatusOK, sources)
}

// apiProxyStatus reports the current proxy-pool scheduler, network and source state.
// @Summary      Proxy status
// @Description  Returns adaptive proxy-pool mode, network health, concurrency, lanes and source statistics.
// @Tags         Proxies
// @Produce      json
// @Success      200 {object} proxypool.Status
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy/status [get]
func (h *Handler) apiProxyStatus(w http.ResponseWriter, _ *http.Request) {
	status, err := h.proxySvc.Status()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, status)
}

// apiProxyRecheckBanned queues manual rechecks for banned proxies.
// @Summary      Recheck banned proxies
// @Description  Queues manual rechecks for banned proxies filtered by reason and source.
// @Tags         Proxies
// @Accept       json
// @Produce      json
// @Param        body body models.RecheckBannedRequest false "Optional ban reason and source filters"
// @Success      202 {object} models.RecheckBannedResponse
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy/recheck-banned [post]
func (h *Handler) apiProxyRecheckBanned(w http.ResponseWriter, r *http.Request) {
	var body models.RecheckBannedRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	queued, err := h.proxySvc.RecheckBanned(body.Reason, body.Source)
	if err != nil {
		if errors.Is(err, proxypool.ErrInvalidBanReason) {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusAccepted, models.RecheckBannedResponse{Queued: queued})
}

// apiProxyPoolsList returns every manually managed proxy pool.
// @Summary      List custom proxy pools
// @Description  Returns manually created proxy pools with their entries.
// @Tags         Proxies
// @Produce      json
// @Success      200 {array} models.CustomProxyPool
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy-pools [get]
func (h *Handler) apiProxyPoolsList(w http.ResponseWriter, _ *http.Request) {
	pools, err := h.proxySvc.ListCustomPools()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if pools == nil {
		pools = []*models.CustomProxyPool{}
	}
	h.json(w, http.StatusOK, pools)
}

// apiProxyPoolsSave creates or replaces a custom proxy pool.
// @Summary      Save custom proxy pool
// @Description  Creates or replaces a manually managed proxy pool and its entries.
// @Tags         Proxies
// @Accept       json
// @Produce      json
// @Param        body body models.CustomProxyPool true "Pool with entries"
// @Success      200 {object} models.CustomProxyPool
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy-pools [post]
func (h *Handler) apiProxyPoolsSave(w http.ResponseWriter, r *http.Request) {
	var body models.CustomProxyPool
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	saved, err := h.proxySvc.SaveCustomPool(&body)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, saved)
}

// apiProxyPoolsDelete removes a custom proxy pool.
// @Summary      Delete custom proxy pool
// @Description  Removes a manually managed proxy pool.
// @Tags         Proxies
// @Param        id path string true "Pool ID"
// @Success      204 "No Content"
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy-pools/{id} [delete]
func (h *Handler) apiProxyPoolsDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.proxySvc.DeleteCustomPool(r.PathValue("id")); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
