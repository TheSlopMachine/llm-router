//go:build !windows && !freebsd

package dashboard

import (
	"encoding/json"
	"net/http"
)

// registerDockerPlatformRoutes adds the local Docker routes of a Linux or
// macOS router.
func (h *Handler) registerDockerPlatformRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/llm-router/dashboard/docker/local", h.requireAuth(h.apiDockerLocal))
	mux.HandleFunc("POST /api/llm-router/dashboard/docker/local/probe", h.requireAuth(h.apiDockerLocalProbe))
}

// apiDockerLocal finds Docker on the router machine.
// @Summary      Find local Docker
// @Description  Lists the Docker sockets of the router machine with their state: DOCKER_HOST, Docker contexts, system and rootless Docker, Podman. Linux and macOS routers only.
// @Tags         Docker
// @Produce      json
// @Success      200 {object} dockerhost.LocalDocker
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/local [get]
func (h *Handler) apiDockerLocal(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	found, err := h.dockerService.LocalDocker(r.Context())
	if err != nil {
		h.dockerError(w, err)
		return
	}
	h.json(w, http.StatusOK, found)
}

// dockerLocalProbeRequest names the socket to probe.
type dockerLocalProbeRequest struct {
	Path string `json:"path"`
}

// apiDockerLocalProbe probes one socket path on the router machine.
// @Summary      Probe a local Docker socket
// @Description  Reports whether the router can use Docker on the socket. Linux and macOS routers only.
// @Tags         Docker
// @Accept       json
// @Produce      json
// @Param        body body dockerLocalProbeRequest true "Socket"
// @Success      200 {object} dockerhost.LocalSocket
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/local/probe [post]
func (h *Handler) apiDockerLocalProbe(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	var body dockerLocalProbeRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	socket, err := h.dockerService.ProbeLocalSocket(r.Context(), body.Path)
	if err != nil {
		h.dockerError(w, err)
		return
	}
	h.json(w, http.StatusOK, socket)
}
