package dashboard

import (
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
)

type pluginView struct {
	ID           string                 `json:"id"`
	DisplayName  string                 `json:"display_name"`
	Author       string                 `json:"author"`
	Version      string                 `json:"version"`
	PluginAPI    string                 `json:"plugin_api_version"`
	Description  string                 `json:"description"`
	License      string                 `json:"license"`
	AllowHosts   []string               `json:"allow_hosts"`
	Unsafe       bool                   `json:"unsafe"`
	TypeKeys     []string               `json:"type_keys"`
	Origin       luaplugin.PluginOrigin `json:"origin"`
	InstalledAt  time.Time              `json:"installed_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
	HistoryCount int                    `json:"history_count"`
}

func toPluginView(record *luaplugin.PluginRecord) pluginView {
	typeKeys := append([]string{}, record.TypeKeys...)
	allowHosts := append([]string{}, record.AllowHosts...)
	sort.Strings(typeKeys)
	return pluginView{
		ID: record.ID, DisplayName: record.DisplayName, Author: record.Author,
		Version: record.Version, PluginAPI: record.PluginAPI,
		Description: record.Description, License: record.License,
		AllowHosts: allowHosts, Unsafe: record.Unsafe,
		TypeKeys: typeKeys, Origin: record.Origin,
		InstalledAt: record.InstalledAt, UpdatedAt: record.UpdatedAt,
		HistoryCount: len(record.History),
	}
}

// apiPluginsList lists installed plugins.
// @Summary      List plugins
// @Description  Returns all installed Lua plugins.
// @Tags         Plugins
// @Produce      json
// @Success      200 {array} object{id=string,display_name=string,author=string,version=string,allow_hosts=[]string,unsafe=bool,type_keys=[]string,enabled=bool}
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
		out = append(out, toPluginView(record))
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
// @Success      200 {object} object{id=string,display_name=string,version=string}
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
	h.json(w, http.StatusOK, toPluginView(record))
}

// apiPluginsInstallFile installs a plugin from uploaded source.
// @Summary      Install plugin file
// @Description  Installs a Lua plugin from uploaded source.
// @Tags         Plugins
// @Accept       plain
// @Produce      json
// @Param        source body string true "Lua plugin source"
// @Success      200 {object} object{id=string,version=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/install-file [post]
func (h *Handler) apiPluginsInstallFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20+1024)
	source, err := io.ReadAll(r.Body)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, "read plugin source: "+err.Error())
		return
	}
	record, err := h.luaService.InstallSQLite(r.Context(), source, luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logInfo("Plugin installed", "plugin_id", record.ID)
	h.json(w, http.StatusOK, toPluginView(record))
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

// apiPluginsRollback rolls a plugin back to its previous version.
// @Summary      Roll back plugin
// @Description  Rolls an installed Lua plugin back to its previous version.
// @Tags         Plugins
// @Produce      json
// @Param        id path string true "Plugin ID"
// @Success      200 {object} object{id=string,version=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id}/rollback [post]
func (h *Handler) apiPluginsRollback(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	record, err := h.luaService.RollbackSQLite(r.Context(), id)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logInfo("Plugin rolled back", "plugin_id", id)
	h.json(w, http.StatusOK, toPluginView(record))
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
