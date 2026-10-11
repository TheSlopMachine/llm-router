package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
)

type repositoryView struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Builtin     bool   `json:"builtin"`
}

func toRepositoryView(record *pluginrepo.RepoRecord) repositoryView {
	return repositoryView{ID: record.ID, URL: record.SourceURL, Title: record.Title, Description: record.Description, Builtin: record.Builtin}
}

// apiReposList lists added plugin repositories.
// @Summary      List plugin repositories
// @Description  Returns all added plugin repositories.
// @Tags         PluginStore
// @Produce      json
// @Success      200 {array} object{id=string,url=string,title=string,description=string,builtin=bool}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugin-repos [get]
func (h *Handler) apiReposList(w http.ResponseWriter, r *http.Request) {
	records, err := h.repositoryService.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]repositoryView, 0, len(records))
	for _, record := range records {
		out = append(out, toRepositoryView(record))
	}
	h.json(w, http.StatusOK, out)
}

// apiReposAdd adds a repository from a repository or index URL.
// @Summary      Add plugin repository
// @Description  Adds a plugin repository from a repository or index URL.
// @Tags         PluginStore
// @Accept       json
// @Produce      json
// @Param        body body object{url=string} true "Repository or index URL"
// @Success      200 {object} object{id=string,url=string,title=string,description=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugin-repos [post]
func (h *Handler) apiReposAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	record, err := h.repositoryService.AddRepo(ctx, body.URL)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, toRepositoryView(record))
}

// apiReposDelete removes a repository.
// @Summary      Remove plugin repository
// @Description  Removes a plugin repository.
// @Tags         PluginStore
// @Produce      json
// @Param        id path string true "Repository ID"
// @Success      200 {object} object{message=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      403 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugin-repos/{id} [delete]
func (h *Handler) apiReposDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.repositoryService.Remove(r.PathValue("id")); err != nil {
		if errors.Is(err, pluginrepo.ErrBuiltinRepoProtected) {
			h.jsonErr(w, http.StatusForbidden, err.Error())
			return
		}
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, map[string]string{"message": "repository removed"})
}

// apiReposFiles lists plugin files with manifests for one repository.
// @Summary      List repository files
// @Description  Lists plugin files with manifests for one repository.
// @Tags         PluginStore
// @Produce      json
// @Param        id path string true "Repository ID"
// @Success      200 {array} object{repo_id=string,path=string,version=string,installed=bool}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugin-repos/{id}/files [get]
func (h *Handler) apiReposFiles(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	files, err := h.repositoryService.ListPluginFiles(r.Context(), id)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, h.describeFiles(r, id, files))
}

// apiStoreSearch aggregates plugin files across all repositories.
// @Summary      Search plugin store
// @Description  Aggregates plugin files across all repositories.
// @Tags         PluginStore
// @Produce      json
// @Success      200 {object} object{repos=array}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugin-store/search [get]
func (h *Handler) apiStoreSearch(w http.ResponseWriter, r *http.Request) {
	repositories, err := h.repositoryService.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type repositoryResult struct {
		view  repositoryView
		files []storeFileView
		err   string
	}
	channel := make(chan repositoryResult, len(repositories))
	for _, record := range repositories {
		go func(record *pluginrepo.RepoRecord) {
			indexDocument, err := h.repositoryService.GetIndex(r.Context(), record.ID)
			view := toRepositoryView(record)
			if err != nil {
				channel <- repositoryResult{view: view, err: err.Error()}
				return
			}
			if indexDocument.Title != "" {
				view.Title = indexDocument.Title
			}
			if indexDocument.Description != "" {
				view.Description = indexDocument.Description
			}
			channel <- repositoryResult{view: view, files: h.describeFiles(r, record.ID, indexDocument.Files)}
		}(record)
	}
	out := make([]map[string]any, 0, len(repositories))
	for range repositories {
		result := <-channel
		if result.files == nil {
			result.files = []storeFileView{}
		}
		out = append(out, map[string]any{
			"repo": result.view, "files": result.files, "error": result.err,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i]["repo"].(repositoryView)
		b, _ := out[j]["repo"].(repositoryView)
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.ID < b.ID
	})
	h.json(w, http.StatusOK, map[string]any{"repos": out})
}

type storeFileView struct {
	RepoID           string   `json:"repo_id"`
	Path             string   `json:"path"`
	DisplayName      string   `json:"display_name"`
	Author           string   `json:"author"`
	Version          string   `json:"version"`
	Description      string   `json:"description"`
	AllowHosts       []string `json:"allow_hosts"`
	Unsafe           bool     `json:"unsafe"`
	Installed        bool     `json:"installed"`
	InstalledVersion string   `json:"installed_version"`
	UpdateAvailable  bool     `json:"update_available"`
	Error            string   `json:"error,omitempty"`
}

func (h *Handler) describeFiles(r *http.Request, repoID string, files []pluginrepo.RepoFile) []storeFileView {
	type fileResult struct {
		view storeFileView
	}
	channel := make(chan fileResult, len(files))
	for _, file := range files {
		go func(file pluginrepo.RepoFile) {
			view := storeFileView{RepoID: repoID, Path: file.Path}
			source, err := h.repositoryService.FetchFile(r.Context(), repoID, file.Path)
			if err != nil {
				view.Error = err.Error()
				channel <- fileResult{view: view}
				return
			}
			manifest, err := luaplugin.ParseManifest(source)
			if err != nil {
				view.Error = "manifest: " + err.Error()
				channel <- fileResult{view: view}
				return
			}
			view.DisplayName = manifest.Plugin
			view.Author = manifest.Author
			view.Version = manifest.Version
			view.Description = manifest.Description
			view.AllowHosts = append([]string{}, manifest.AllowHosts...)
			view.Unsafe = manifest.Unsafe
			if id, err := luaplugin.BuildID(luaplugin.PluginOrigin{RepoID: repoID, Path: file.Path}, manifest); err == nil {
				if existing, err := h.luaService.Get(id); err == nil && existing != nil {
					view.Installed = true
					view.InstalledVersion = existing.Version
					if comparison, comparisonError := luaplugin.CompareVersions(manifest.Version, existing.Version); comparisonError == nil && comparison > 0 {
						view.UpdateAvailable = true
					}
				}
			}
			channel <- fileResult{view: view}
		}(file)
	}
	out := make([]storeFileView, 0, len(files))
	for range files {
		out = append(out, (<-channel).view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// apiPluginsInstallFromRepo downloads a file and installs it.
// @Summary      Install plugin from repository
// @Description  Downloads a plugin file and installs it.
// @Tags         Plugins
// @Accept       json
// @Produce      json
// @Param        body body object{repo_id=string,path=string} true "Plugin location"
// @Success      200 {object} object{id=string,version=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugins/install-from-repo [post]
func (h *Handler) apiPluginsInstallFromRepo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RepoID string `json:"repo_id"`
		Path   string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.RepoID == "" || body.Path == "" {
		h.jsonErr(w, http.StatusBadRequest, "repo_id and path are required")
		return
	}
	source, err := h.repositoryService.FetchFile(r.Context(), body.RepoID, body.Path)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	record, err := h.luaService.Install(source, luaplugin.PluginOrigin{RepoID: body.RepoID, Path: body.Path})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, toPluginView(record))
}

// apiStoreUpdates lists installed repo-backed plugins with newer upstream versions.
// @Summary      Check plugin updates
// @Description  Lists installed repository plugins with newer upstream versions.
// @Tags         PluginStore
// @Produce      json
// @Success      200 {object} object{updates=array}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugin-store/updates [get]
func (h *Handler) apiStoreUpdates(w http.ResponseWriter, r *http.Request) {
	installed, err := h.luaService.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type updateView struct {
		PluginID        string   `json:"plugin_id"`
		Current         string   `json:"current"`
		Latest          string   `json:"latest"`
		RepoID          string   `json:"repo_id"`
		Path            string   `json:"path"`
		NewAllowHosts   []string `json:"new_allow_hosts"`
		NewUnsafe       bool     `json:"new_unsafe"`
		UpdateAvailable bool     `json:"update_available"`
		Error           string   `json:"error,omitempty"`
	}
	channel := make(chan updateView, len(installed))
	count := 0
	for _, record := range installed {
		if record.Origin.Manual || record.Origin.RepoID == "" {
			continue
		}
		count++
		go func(record *luaplugin.PluginRecord) {
			view := updateView{PluginID: record.ID, Current: record.Version, RepoID: record.Origin.RepoID, Path: record.Origin.Path, NewAllowHosts: []string{}}
			source, err := h.repositoryService.FetchFile(r.Context(), record.Origin.RepoID, record.Origin.Path)
			if err != nil {
				view.Error = err.Error()
				channel <- view
				return
			}
			manifest, err := luaplugin.ParseManifest(source)
			if err != nil {
				view.Error = err.Error()
				channel <- view
				return
			}
			view.Latest = manifest.Version
			view.NewAllowHosts = append([]string{}, manifest.AllowHosts...)
			view.NewUnsafe = manifest.Unsafe
			if comparison, comparisonError := luaplugin.CompareVersions(manifest.Version, record.Version); comparisonError == nil && comparison > 0 {
				view.UpdateAvailable = true
			}
			channel <- view
		}(record)
	}
	out := make([]updateView, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, <-channel)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PluginID < out[j].PluginID })
	h.json(w, http.StatusOK, map[string]any{"updates": out})
}
