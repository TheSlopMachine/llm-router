package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/services/dockerhost"
)

// dockerErrorBody is a Docker failure: the message, the fault reason and,
// for connection failures, the stage that failed.
type dockerErrorBody struct {
	Error  string `json:"error"`
	Reason string `json:"reason,omitempty"`
	Stage  string `json:"stage,omitempty"`
}

// dockerReady answers 503 when the Docker service is not wired.
func (h *Handler) dockerReady(w http.ResponseWriter) bool {
	if h.dockerService == nil {
		h.jsonErr(w, http.StatusServiceUnavailable, "docker service is not configured")
		return false
	}
	return true
}

// dockerError renders a Docker service error with its status.
func (h *Handler) dockerError(w http.ResponseWriter, err error) {
	var specErr *dockerhost.SpecError
	switch {
	case errors.As(err, &specErr), errors.Is(err, dockerhost.ErrInvalidSettings):
		h.jsonErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, apierrors.ErrNotFound):
		h.jsonErr(w, http.StatusNotFound, err.Error())
	default:
		if fault, ok := dockerhost.AsFault(err); ok {
			h.json(w, fault.Status(), dockerErrorBody{Error: fault.Message, Reason: fault.Reason, Stage: fault.Stage})
			return
		}
		h.logError("Docker request failed", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
	}
}

// apiDockerConnectionGet returns the Docker connection settings.
// @Summary      Get Docker connection
// @Description  Returns the Docker daemon connection. Secrets show only whether they are set.
// @Tags         Docker
// @Produce      json
// @Success      200 {object} dockerhost.ConnectionSettings
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/connection [get]
func (h *Handler) apiDockerConnectionGet(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	settings, err := h.dockerService.Connection(r.Context())
	if err != nil {
		h.dockerError(w, err)
		return
	}
	h.json(w, http.StatusOK, settings)
}

// apiDockerConnectionPut stores the Docker connection settings.
// @Summary      Update Docker connection
// @Description  Stores the Docker daemon connection. A null secret keeps the stored value; an empty string clears it.
// @Tags         Docker
// @Accept       json
// @Produce      json
// @Param        body body dockerhost.ConnectionUpdate true "Connection"
// @Success      200 {object} dockerhost.ConnectionSettings
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/connection [put]
func (h *Handler) apiDockerConnectionPut(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	var body dockerhost.ConnectionUpdate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	settings, err := h.dockerService.PutConnection(r.Context(), body)
	if err != nil {
		h.dockerError(w, err)
		return
	}
	h.json(w, http.StatusOK, settings)
}

// apiDockerConnectionTest connects with the stored settings.
// @Summary      Test Docker connection
// @Description  Connects to the Docker daemon with the stored settings and prepares the gateway image.
// @Tags         Docker
// @Produce      json
// @Success      200 {object} dockerhost.ConnectionTest
// @Failure      401 {object} models.ErrorResponse
// @Failure      502 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/connection/test [post]
func (h *Handler) apiDockerConnectionTest(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	result, err := h.dockerService.TestConnection(r.Context())
	if err != nil {
		h.dockerError(w, err)
		return
	}
	h.json(w, http.StatusOK, result)
}

// dockerHostKeyRequest names the endpoint to scan.
type dockerHostKeyRequest struct {
	Endpoint string `json:"endpoint"`
}

// apiDockerHostKeyScan reads the SSH host key of an endpoint.
// @Summary      Scan Docker SSH host key
// @Description  Reads the SSH host key of an ssh:// endpoint for confirmation. Stores nothing.
// @Tags         Docker
// @Accept       json
// @Produce      json
// @Param        body body dashboard.dockerHostKeyRequest true "Endpoint"
// @Success      200 {object} dockerhost.HostKeyScan
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/host-key [post]
func (h *Handler) apiDockerHostKeyScan(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	var body dockerHostKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	scan, err := h.dockerService.ScanHostKey(r.Context(), body.Endpoint)
	if err != nil {
		if errors.Is(err, dockerhost.ErrInvalidSettings) {
			h.dockerError(w, err)
			return
		}
		h.jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	h.json(w, http.StatusOK, scan)
}

// apiDockerRegistriesList lists registry logins.
// @Summary      List Docker registry logins
// @Description  Lists registry logins without secrets.
// @Tags         Docker
// @Produce      json
// @Success      200 {array} dockerhost.RegistryCredential
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/registries [get]
func (h *Handler) apiDockerRegistriesList(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	list, err := h.dockerService.RegistryCredentials(r.Context())
	if err != nil {
		h.dockerError(w, err)
		return
	}
	h.json(w, http.StatusOK, list)
}

// apiDockerRegistryPut stores one registry login.
// @Summary      Store Docker registry login
// @Description  Stores the login of one registry host such as docker.io or ghcr.io.
// @Tags         Docker
// @Accept       json
// @Produce      json
// @Param        host path string true "Registry host"
// @Param        body body dockerhost.RegistryCredentialUpdate true "Login"
// @Success      200 {object} dockerhost.RegistryCredential
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/registries/{host} [put]
func (h *Handler) apiDockerRegistryPut(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	var body dockerhost.RegistryCredentialUpdate
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	stored, err := h.dockerService.PutRegistryCredential(r.Context(), r.PathValue("host"), body)
	if err != nil {
		h.dockerError(w, err)
		return
	}
	h.json(w, http.StatusOK, stored)
}

// apiDockerRegistryDelete removes one registry login.
// @Summary      Delete Docker registry login
// @Description  Removes the login of one registry host.
// @Tags         Docker
// @Produce      json
// @Param        host path string true "Registry host"
// @Success      200 {object} object{message=string}
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/docker/registries/{host} [delete]
func (h *Handler) apiDockerRegistryDelete(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	if err := h.dockerService.DeleteRegistryCredential(r.Context(), r.PathValue("host")); err != nil {
		h.dockerError(w, err)
		return
	}
	h.json(w, http.StatusOK, map[string]string{"message": "registry login deleted"})
}

// pluginDockerPolicy resolves the Docker policy of the plugin in the path.
// It answers 404 for unknown plugins and plugins without @require_api docker.
func (h *Handler) pluginDockerPolicy(w http.ResponseWriter, r *http.Request) (dockerhost.Policy, bool) {
	if !h.dockerReady(w) {
		return dockerhost.Policy{}, false
	}
	policy, declared, err := h.luaService.DockerPolicy(r.Context(), r.PathValue("id"))
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, "plugin not found")
		return dockerhost.Policy{}, false
	}
	if !declared {
		h.jsonErr(w, http.StatusNotFound, "plugin does not use docker")
		return dockerhost.Policy{}, false
	}
	return policy, true
}

// providerDockerView is the Docker state behind one provider. View is set
// only when the provider's plugin declares @require_api docker.
type providerDockerView struct {
	PluginID   string                 `json:"plugin_id"`
	UsesDocker bool                   `json:"uses_docker"`
	View       *dockerhost.PluginView `json:"view,omitempty"`
}

// apiProviderDocker returns the Docker state of the plugin behind one provider.
// @Summary      Get provider Docker state
// @Description  Returns the plugin behind a provider and, when it uses Docker, its directives, image progress and containers.
// @Tags         Docker
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {object} dashboard.providerDockerView
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/docker [get]
func (h *Handler) apiProviderDocker(w http.ResponseWriter, r *http.Request) {
	if !h.dockerReady(w) {
		return
	}
	instance, err := h.gormProviderService.Get(r.Context(), r.PathValue("id"))
	if err != nil || instance == nil {
		h.jsonErr(w, http.StatusNotFound, "provider not found")
		return
	}
	record, err := h.luaService.PluginForType(r.Context(), instance.TypeKey)
	if err != nil {
		h.dockerError(w, err)
		return
	}
	if record == nil {
		h.json(w, http.StatusOK, providerDockerView{})
		return
	}
	policy, declared, err := h.luaService.DockerPolicy(r.Context(), record.ID)
	if err != nil {
		h.dockerError(w, err)
		return
	}
	answer := providerDockerView{PluginID: record.ID, UsesDocker: declared}
	if declared {
		view, err := h.dockerService.View(r.Context(), policy)
		if err != nil {
			h.dockerError(w, err)
			return
		}
		answer.View = &view
	}
	h.json(w, http.StatusOK, answer)
}

// apiPluginDockerAction starts, stops or removes one plugin container.
// @Summary      Run plugin container action
// @Description  Starts, stops or removes one container or compose project of a plugin.
// @Tags         Docker
// @Produce      json
// @Param        id path string true "Plugin ID"
// @Param        action path string true "start, stop or remove"
// @Param        target query string true "Container key or project/service"
// @Success      200 {object} object{message=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Failure      503 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id}/docker/containers/{action} [post]
func (h *Handler) apiPluginDockerAction(w http.ResponseWriter, r *http.Request) {
	policy, ok := h.pluginDockerPolicy(w, r)
	if !ok {
		return
	}
	action := r.PathValue("action")
	if err := h.dockerService.OperatorAction(r.Context(), policy, r.URL.Query().Get("target"), action); err != nil {
		h.dockerError(w, err)
		return
	}
	h.logInfo("Plugin container action done", "plugin_id", policy.PluginID, "operation", action, "container_key", r.URL.Query().Get("target"))
	h.json(w, http.StatusOK, map[string]string{"message": "done"})
}

// apiPluginDockerLogs returns the last log lines of one plugin container.
// @Summary      Get plugin container logs
// @Description  Returns the last log lines of one container, stdout and stderr merged.
// @Tags         Docker
// @Produce      json
// @Param        id path string true "Plugin ID"
// @Param        target query string true "Container key or project/service"
// @Param        tail query int false "Line count, default 200"
// @Success      200 {object} object{logs=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id}/docker/logs [get]
func (h *Handler) apiPluginDockerLogs(w http.ResponseWriter, r *http.Request) {
	policy, ok := h.pluginDockerPolicy(w, r)
	if !ok {
		return
	}
	tail := 200
	if raw := r.URL.Query().Get("tail"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 10000 {
			h.jsonErr(w, http.StatusBadRequest, "tail must be a whole number from 1 to 10000")
			return
		}
		tail = parsed
	}
	logs, err := h.dockerService.Logs(r.Context(), policy, r.URL.Query().Get("target"), tail)
	if err != nil {
		h.dockerError(w, err)
		return
	}
	h.json(w, http.StatusOK, map[string]string{"logs": logs})
}
