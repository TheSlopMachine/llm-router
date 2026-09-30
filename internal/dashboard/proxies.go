package dashboard

import (
	"net/http"

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

// apiProxyRefresh requests a full library refresh.
// @Summary      Refresh proxy pool
// @Description  Requests a background refresh of all registered proxy sources.
// @Tags         Proxies
// @Produce      json
// @Success      202 {object} object{started=bool,reason=string}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxies/refresh [post]
func (h *Handler) apiProxyRefresh(w http.ResponseWriter, _ *http.Request) {
	started := h.proxySvc.RequestRefresh()
	response := map[string]any{"started": started}
	if !started {
		response["reason"] = "refresh already running"
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

// apiProxyStatus reports library cache and refresh schedule state.
// @Summary      Proxy status
// @Description  Returns cached proxy counts and the next scheduled refresh.
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
