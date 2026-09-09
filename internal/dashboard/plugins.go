package dashboard

import (
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
)

type pluginView struct {
	ID            string                 `json:"id"`
	DisplayName   string                 `json:"display_name"`
	Author        string                 `json:"author"`
	Version       string                 `json:"version"`
	RouterVersion string                 `json:"router_version"`
	Description   string                 `json:"description"`
	License       string                 `json:"license"`
	AllowHosts    []string               `json:"allow_hosts"`
	Unsafe        bool                   `json:"unsafe"`
	TypeKeys      []string               `json:"type_keys"`
	Origin        luaplugin.PluginOrigin `json:"origin"`
	InstalledAt   time.Time              `json:"installed_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	HistoryCount  int                    `json:"history_count"`
}

func toPluginView(rec *luaplugin.PluginRecord) pluginView {
	typeKeys := append([]string{}, rec.TypeKeys...)
	allowHosts := append([]string{}, rec.AllowHosts...)
	sort.Strings(typeKeys)
	return pluginView{
		ID: rec.ID, DisplayName: rec.DisplayName, Author: rec.Author,
		Version: rec.Version, RouterVersion: rec.RouterVersion,
		Description: rec.Description, License: rec.License,
		AllowHosts: allowHosts, Unsafe: rec.Unsafe,
		TypeKeys: typeKeys, Origin: rec.Origin,
		InstalledAt: rec.InstalledAt, UpdatedAt: rec.UpdatedAt,
		HistoryCount: len(rec.History),
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
	records, err := h.luaSvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]pluginView, 0, len(records))
	for _, rec := range records {
		out = append(out, toPluginView(rec))
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
	rec, err := h.luaSvc.Get(r.PathValue("id"))
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, "plugin not found")
		return
	}
	h.json(w, http.StatusOK, toPluginView(rec))
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
	rec, err := h.luaSvc.Install(source, luaplugin.PluginOrigin{Manual: true})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, toPluginView(rec))
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
	if err := h.luaSvc.Delete(r.PathValue("id")); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
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
	rec, err := h.luaSvc.Rollback(r.PathValue("id"))
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, toPluginView(rec))
}

// apiPluginsLogs returns recent print() output for a plugin.
// @Summary      Get plugin logs
// @Description  Returns recent print() output for a plugin.
// @Tags         Plugins
// @Produce      json
// @Param        id path string true "Plugin ID"
// @Success      200 {array} object{at=string,message=string}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/{id}/logs [get]
func (h *Handler) apiPluginsLogs(w http.ResponseWriter, r *http.Request) {
	h.json(w, http.StatusOK, h.luaSvc.Logs(r.PathValue("id")))
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
	h.json(w, http.StatusOK, h.luaSvc.Crashes(r.PathValue("id")))
}
