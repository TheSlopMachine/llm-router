package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/proxypool"
)

// apiProxiesList returns the full proxy pool
// @Summary      List proxies
// @Description  Returns all pooled proxies, manual first, fastest first.
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
// @Description  Registers a user-provided proxy (http/https/socks4/socks5, optional auth). The pool probes it at once; dead or over-cap entries are refused.
// @Tags         Proxies
// @Accept       json
// @Produce      json
// @Param        body body object{url=string,location=string} true "Proxy URL"
// @Success      200 {object} models.Proxy
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxies [post]
func (h *Handler) apiProxiesAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL      string `json:"url"`
		Location string `json:"location"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.URL == "" {
		h.jsonErr(w, http.StatusBadRequest, "url is required")
		return
	}
	p, err := h.proxySvc.AddManual(body.URL, body.Location)
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

// apiProxySources lists proxy-list source plugins with fetch stats
// @Summary      List proxy sources
// @Description  Returns every registered source with its fetch status and counters.
// @Tags         Proxies
// @Produce      json
// @Success      200 {array} proxypool.SourceInfo
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy-sources [get]
func (h *Handler) apiProxySources(w http.ResponseWriter, r *http.Request) {
	keys := h.luaSvc.ProxySourceKeys()
	if keys == nil {
		keys = []string{}
	}
	h.json(w, http.StatusOK, h.proxySvc.SourceInfos(keys))
}

// apiProxySourceRefresh starts an async refresh cycle for a source:
// fetch the list, add candidates, then rotate the whole pool. A full pool
// short-circuits: nothing to fill, nothing runs.
// @Summary      Refresh proxy source
// @Description  Starts a background refresh: candidates are probed as they arrive and added under the pool rules, then the pool rotates.
// @Tags         Proxies
// @Param        key path string true "Source type key"
// @Success      202 {object} object{started=bool,reason=string}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy-sources/{key}/refresh [post]
func (h *Handler) apiProxySourceRefresh(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !h.proxySvc.NeedsSearch() {
		h.json(w, http.StatusAccepted, map[string]any{"started": false, "reason": "pool full"})
		return
	}
	h.proxySvc.SetSourceFetching(key)
	go func() {
		// Detached context: this work outlives the request by design
		// (202 Accepted). r.Context() dies with the handler return.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		candidates, err := h.luaSvc.FetchProxies(ctx, key)
		if err != nil {
			h.proxySvc.SetSourceFailed(key, err)
			return
		}
		if err := h.proxySvc.AddCandidates(ctx, key, candidates); err != nil {
			// A busy pool means another rotation is already working through
			// these candidates; not a source failure.
			if errors.Is(err, proxypool.ErrBusy) {
				h.proxySvc.SetSourceDone(key, len(candidates))
				return
			}
			h.proxySvc.SetSourceFailed(key, err)
			return
		}
		h.proxySvc.SetSourceRotating(key)
		if err := h.proxySvc.RotateAll(ctx); err != nil {
			// Adds already landed; a failed rotation must not masquerade
			// as a failed fetch.
			h.logger.Warn("proxy source refresh: rotation failed", "source", key, "err", err)
			h.proxySvc.SetSourceDone(key, len(candidates))
			return
		}
		h.proxySvc.SetSourceDone(key, len(candidates))
	}()
	h.json(w, http.StatusAccepted, map[string]any{"started": true})
}

// apiProxySourceProxies returns pooled proxies of one source
// @Summary      List source proxies
// @Description  Pooled proxies pulled from one source, fastest first, paginated.
// @Tags         Proxies
// @Produce      json
// @Param        key path string true "Source type key"
// @Param        offset query int false "Offset"
// @Param        limit query int false "Limit (default 100)"
// @Success      200 {object} object{items=array,total=int}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy-sources/{key}/proxies [get]
func (h *Handler) apiProxySourceProxies(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 100
	}
	items, total, err := h.proxySvc.SourceProxies(key, offset, limit)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{"items": items, "total": total})
}

// apiProxyStatus reports pool stats
// @Summary      Proxy status
// @Description  total counts every proxy the pool holds; searching reports whether the next scheduled fetch will run; checking reports live probe work.
// @Produce      json
// @Success      200 {object} object{total=int,searching=bool,checking=bool}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/proxy/status [get]
func (h *Handler) apiProxyStatus(w http.ResponseWriter, r *http.Request) {
	total, err := h.proxySvc.PoolTotals()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]any{
		"total":     total,
		"searching": h.proxySvc.NeedsSearch(),
		"checking":  h.proxySvc.Checking(),
	})
}
