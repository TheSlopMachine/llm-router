package dashboard

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	stdErrors "errors"

	apierrors "github.com/TheSlopMachine/llm-router/internal/errors"
	"github.com/TheSlopMachine/llm-router/internal/services/datamanagement"
	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
)

type pluginView struct {
	ID          string                 `json:"id"`
	DisplayName string                 `json:"display_name"`
	Author      string                 `json:"author"`
	Version     string                 `json:"version"`
	PluginAPI   string                 `json:"plugin_api_version"`
	Description string                 `json:"description"`
	License     string                 `json:"license"`
	Permissions luaplugin.Permissions  `json:"permissions"`
	Disabled    bool                   `json:"disabled"`
	TypeKeys    []string               `json:"type_keys"`
	Subsystems  []string               `json:"subsystems"`
	IconURL     string                 `json:"icon_url"`
	Origin      luaplugin.PluginOrigin `json:"origin"`
	InstalledAt time.Time              `json:"installed_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

func (h *Handler) pluginView(record *luaplugin.PluginRecord) (pluginView, error) {
	typeKeys := append([]string{}, record.TypeKeys...)
	sort.Strings(typeKeys)
	permissions, err := h.luaService.Permissions(record)
	if err != nil {
		return pluginView{}, err
	}
	return pluginView{
		ID: record.ID, DisplayName: record.DisplayName, Author: record.Author,
		Version: record.Version, PluginAPI: record.PluginAPI,
		Description: record.Description, License: record.License,
		Permissions: permissions, Disabled: record.Disabled,
		TypeKeys: typeKeys, Subsystems: record.Subsystems(), IconURL: record.Icon(), Origin: record.Origin,
		InstalledAt: record.InstalledAt, UpdatedAt: record.UpdatedAt,
	}, nil
}

// writePluginView answers with the view of one plugin record.
func (h *Handler) writePluginView(w http.ResponseWriter, record *luaplugin.PluginRecord) {
	view, err := h.pluginView(record)
	if err != nil {
		h.logError("Failed to read plugin manifest", "plugin_id", record.ID, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, view)
}

// apiPluginsList lists installed plugins.
// @Summary      List plugins
// @Description  Returns all installed Lua plugins.
// @Tags         Plugins
// @Produce      json
// @Success      200 {array} object{id=string,display_name=string,author=string,version=string,allow_hosts=[]string,unsafe=bool,disabled=bool,type_keys=[]string,subsystems=[]string,icon_url=string}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins [get]
func (h *Handler) apiPluginsList(w http.ResponseWriter, r *http.Request) {
	records, err := h.luaService.ListSQLite(r.Context())
	if err != nil {
		h.logError("Failed to list plugins", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]pluginView, 0, len(records))
	for _, record := range records {
		view, err := h.pluginView(record)
		if err != nil {
			h.logError("Failed to read plugin manifest", "plugin_id", record.ID, "error", err)
			h.jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	h.json(w, http.StatusOK, out)
}

// apiPluginsGet returns one installed plugin.
// @Summary      Get plugin
// @Description  Returns one installed Lua plugin.
// @Tags         Plugins
// @Produce      json
// @Param        id path string true "Plugin ID"
// @Success      200 {object} object{id=string,display_name=string,version=string,disabled=bool}
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id} [get]
func (h *Handler) apiPluginsGet(w http.ResponseWriter, r *http.Request) {
	record, err := h.luaService.GetSQLite(r.Context(), r.PathValue("id"))
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, "plugin not found")
		return
	}
	h.writePluginView(w, record)
}

// apiPluginsSource returns the stored Lua source of one installed plugin.
// @Summary      Get plugin source
// @Description  Returns the stored Lua source of one installed plugin as a <plugin id>.lua attachment.
// @Tags         Plugins
// @Produce      plain
// @Param        id path string true "Plugin ID"
// @Success      200 {string} string "Lua plugin source"
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id}/source [get]
func (h *Handler) apiPluginsSource(w http.ResponseWriter, r *http.Request) {
	record, err := h.luaService.GetSQLite(r.Context(), r.PathValue("id"))
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, "plugin not found")
		return
	}
	fileName := strings.ReplaceAll(datamanagement.PluginSourceFileName(record.ID), "/", "_")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileName}))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(record.Source); err != nil {
		h.logError("Failed to write plugin source", "plugin_id", record.ID, "error", err)
	}
}

// apiPluginsUpdate flips the operator-owned disabled flag of one plugin.
// Disabled plugins stay inventoried but leave the type registry; requests
// for their types fail with provider_disabled. Disable fires deinitialize
// with reason disable; enable fires initialize with reason enable.
// @Summary      Update plugin
// @Description  Enables or disables one installed Lua plugin.
// @Tags         Plugins
// @Accept       json
// @Produce      json
// @Param        id path string true "Plugin ID"
// @Param        body body object{disabled=bool} true "Disabled state"
// @Success      200 {object} object{id=string,display_name=string,version=string,disabled=bool}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      404 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id} [put]
func (h *Handler) apiPluginsUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		h.jsonErr(w, http.StatusBadRequest, "id is required")
		return
	}
	var body struct {
		Disabled *bool `json:"disabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Disabled == nil {
		h.jsonErr(w, http.StatusBadRequest, "nothing to update")
		return
	}
	record, err := h.luaService.SetDisabled(r.Context(), id, *body.Disabled)
	if err != nil {
		switch {
		case stdErrors.Is(err, apierrors.ErrNotFound):
			h.jsonErr(w, http.StatusNotFound, "plugin not found")
		case stdErrors.Is(err, luaplugin.ErrDockerUnconfigured):
			h.jsonErr(w, http.StatusConflict, err.Error())
		default:
			h.jsonErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	h.logInfo("Plugin admin state changed", "plugin_id", id, "disabled", *body.Disabled)
	h.writePluginView(w, record)
}

// readPluginSource reads an uploaded plugin source.
func (h *Handler) readPluginSource(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20+1024)
	source, err := io.ReadAll(r.Body)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, "read plugin source: "+err.Error())
		return nil, false
	}
	return source, true
}

// installPlugin installs source once it requests the approved permissions.
// An empty digest approves nothing: API clients install without a review.
func (h *Handler) installPlugin(w http.ResponseWriter, r *http.Request, source []byte, origin luaplugin.PluginOrigin, approved string) (*luaplugin.PluginRecord, bool) {
	if err := luaplugin.CheckApproval(source, approved); err != nil {
		status := http.StatusBadRequest
		if stdErrors.Is(err, luaplugin.ErrPermissionsChanged) {
			status = http.StatusConflict
		}
		h.jsonErr(w, status, err.Error())
		return nil, false
	}
	record, err := h.luaService.InstallSQLite(r.Context(), source, origin)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return record, true
}

// pluginPreview is what an uploaded plugin would install as.
type pluginPreview struct {
	ID          string                `json:"id"`
	DisplayName string                `json:"display_name"`
	Version     string                `json:"version"`
	Permissions luaplugin.Permissions `json:"permissions"`
}

// apiPluginsPreviewFile reads an uploaded plugin without installing it.
// @Summary      Preview plugin file
// @Description  Returns the identity and requested permissions of an uploaded Lua plugin without installing it.
// @Tags         Plugins
// @Accept       plain
// @Produce      json
// @Param        source body string true "Lua plugin source"
// @Success      200 {object} dashboard.pluginPreview
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/preview-file [post]
func (h *Handler) apiPluginsPreviewFile(w http.ResponseWriter, r *http.Request) {
	source, ok := h.readPluginSource(w, r)
	if !ok {
		return
	}
	manifest, err := luaplugin.ParseManifest(source)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, "manifest: "+err.Error())
		return
	}
	if err := luaplugin.CheckPluginAPI(manifest); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	id, err := luaplugin.BuildID(luaplugin.PluginOrigin{Manual: true}, manifest)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, pluginPreview{ID: id, DisplayName: manifest.Plugin, Version: manifest.Version, Permissions: manifest.Permissions()})
}

// apiPluginsInstallFile installs a plugin from uploaded source.
// @Summary      Install plugin file
// @Description  Installs a Lua plugin from uploaded source. With permissions_digest set, the source must request exactly the permissions the operator approved.
// @Tags         Plugins
// @Accept       plain
// @Produce      json
// @Param        source body string true "Lua plugin source"
// @Param        permissions_digest query string false "Digest of the approved permissions"
// @Success      200 {object} object{id=string,version=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      409 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/install-file [post]
func (h *Handler) apiPluginsInstallFile(w http.ResponseWriter, r *http.Request) {
	source, ok := h.readPluginSource(w, r)
	if !ok {
		return
	}
	record, ok := h.installPlugin(w, r, source, luaplugin.PluginOrigin{Manual: true}, r.URL.Query().Get("permissions_digest"))
	if !ok {
		return
	}
	h.logInfo("Plugin installed", "plugin_id", record.ID)
	h.writePluginView(w, record)
}

// apiPluginsDelete removes an installed plugin.
// @Summary      Delete plugin
// @Description  Removes an installed Lua plugin.
// @Tags         Plugins
// @Produce      json
// @Param        id path string true "Plugin ID"
// @Success      200 {object} object{message=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id} [delete]
func (h *Handler) apiPluginsDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.luaService.DeleteSQLite(r.Context(), id); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logInfo("Plugin deleted", "plugin_id", id)
	h.json(w, http.StatusOK, map[string]string{"message": "plugin deleted"})
}

// apiPluginsLogs returns recent plugin log lines from the events database.
// @Summary      Get plugin logs
// @Description  Returns recent plugin log lines with severity and keys.
// @Tags         Plugins
// @Produce      json
// @Param        id path string true "Plugin ID"
// @Success      200 {array} luaplugin.LogEntry
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id}/logs [get]
func (h *Handler) apiPluginsLogs(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	entries, err := h.luaService.Logs(r.Context(), id)
	if err != nil {
		h.logError("Failed to read plugin logs", "plugin_id", id, "error", err)
		h.jsonErr(w, http.StatusServiceUnavailable, "plugin logs unavailable")
		return
	}
	h.json(w, http.StatusOK, entries)
}

// apiPluginsCrashes returns recent recorded crashes for a plugin.
// @Summary      Get plugin crashes
// @Description  Returns recent recorded crashes for a plugin.
// @Tags         Plugins
// @Produce      json
// @Param        id path string true "Plugin ID"
// @Success      200 {array} object{at=string,type_key=string,cause=string}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id}/crashes [get]
func (h *Handler) apiPluginsCrashes(w http.ResponseWriter, r *http.Request) {
	h.json(w, http.StatusOK, h.luaService.Crashes(r.PathValue("id")))
}
