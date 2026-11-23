package dashboard

import (
	"errors"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/services/dockerhost"
)

// registerDockerPlatformRoutes adds the WSL setup routes of a Windows router.
func (h *Handler) registerDockerPlatformRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/llm-router/dashboard/docker/wsl/distributions", h.requireAuth(h.apiDockerWSLDistributions))
	mux.HandleFunc("GET /api/llm-router/dashboard/docker/wsl/distributions/{name}", h.requireAuth(h.apiDockerWSLStatus))
	mux.HandleFunc("POST /api/llm-router/dashboard/docker/wsl/distributions/{name}/setup", h.requireAuth(h.apiDockerWSLSetup))
}

// dockerWSLError renders a failed wsl.exe call as 502 and every other error
// as dockerError does.
func (h *Handler) dockerWSLError(w http.ResponseWriter, err error) {
	if errors.Is(err, dockerhost.ErrWSLCommand) {
		h.jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	h.dockerError(w, err)
}

// apiDockerWSLDistributions lists the WSL distros of the router machine.
// @Summary      List WSL distros
// @Description  Lists the WSL distros of the Windows machine the router runs on. Docker Desktop distros are left out. Windows routers only.
// @Tags         Docker
// @Produce      json
// @Success      200 {array} dockerhost.WSLDistribution
// @Failure      401 {object} models.ErrorResponse
// @Failure      502 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/wsl/distributions [get]
func (h *Handler) apiDockerWSLDistributions(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	distributions, err := h.dockerService.WSLDistributions(r.Context())
	if err != nil {
		h.dockerWSLError(w, err)
		return
	}
	h.json(w, http.StatusOK, distributions)
}

// apiDockerWSLStatus reports Docker inside one WSL distro.
// @Summary      Get Docker state in a WSL distro
// @Description  Reports whether Docker in the distro runs as a service on localhost. Starts a stopped distro. Windows routers only.
// @Tags         Docker
// @Produce      json
// @Param        name path string true "Distro name"
// @Success      200 {object} dockerhost.WSLDockerStatus
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      502 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/wsl/distributions/{name} [get]
func (h *Handler) apiDockerWSLStatus(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	status, err := h.dockerService.WSLDocker(r.Context(), r.PathValue("name"))
	if err != nil {
		h.dockerWSLError(w, err)
		return
	}
	h.json(w, http.StatusOK, status)
}

// apiDockerWSLSetup sets up Docker inside one WSL distro.
// @Summary      Set up Docker in a WSL distro
// @Description  Runs Docker in the distro as a service that also listens on 127.0.0.1:2375. Turns systemd on first when it is off, which restarts the distro. Running containers of the distro stop. Windows routers only.
// @Tags         Docker
// @Produce      json
// @Param        name path string true "Distro name"
// @Success      200 {object} dockerhost.WSLDockerStatus
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      502 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/wsl/distributions/{name}/setup [post]
func (h *Handler) apiDockerWSLSetup(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	status, err := h.dockerService.SetUpWSLDocker(r.Context(), r.PathValue("name"))
	if err != nil {
		h.dockerWSLError(w, err)
		return
	}
	h.json(w, http.StatusOK, status)
}
