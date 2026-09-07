package dashboard

import (
	"encoding/json"
	"net/http"
	"sort"

	"github.com/TheSlopMachine/llm-router/internal/services/luaplugin"
	"github.com/TheSlopMachine/llm-router/internal/services/pluginrepo"
)

type repoView struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Owner    string `json:"owner"`
	Repo     string `json:"repo"`
	IndexURL string `json:"index_url"`
}

func toRepoView(rec *pluginrepo.RepoRecord) repoView {
	return repoView{ID: rec.ID, Kind: rec.Kind, Owner: rec.Owner, Repo: rec.Repo, IndexURL: rec.IndexURL}
}

// apiReposList lists added plugin repositories.
// @Summary      List plugin repositories
// @Description  Returns all added plugin repositories.
// @Tags         PluginStore
// @Produce      json
// @Success      200 {array} object{id=string,kind=string,owner=string,repo=string,index_url=string}
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugin-repos [get]
func (h *Handler) apiReposList(w http.ResponseWriter, r *http.Request) {
	records, err := h.repoSvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]repoView, 0, len(records))
	for _, rec := range records {
		out = append(out, toRepoView(rec))
	}
	h.json(w, http.StatusOK, out)
}

// apiReposAdd adds a repository by kind.
// @Summary      Add plugin repository
// @Description  Adds a plugin repository by kind.
// @Tags         PluginStore
// @Accept       json
// @Produce      json
// @Param        body body object{kind=string,owner=string,repo=string,index_url=string} true "Repository details"
// @Success      200 {object} object{id=string,kind=string}
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugin-repos [post]
func (h *Handler) apiReposAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind     string `json:"kind"`
		Owner    string `json:"owner"`
		Repo     string `json:"repo"`
		IndexURL string `json:"index_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ctx := r.Context()
	var rec *pluginrepo.RepoRecord
	var err error
	switch body.Kind {
	case "github":
		rec, err = h.repoSvc.AddGitHub(ctx, body.Owner, body.Repo)
	case "generic-index":
		rec, err = h.repoSvc.AddGeneric(ctx, body.IndexURL)
	default:
		h.jsonErr(w, http.StatusBadRequest, "kind must be github or generic-index")
		return
	}
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, toRepoView(rec))
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
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/plugin-repos/{id} [delete]
func (h *Handler) apiReposDelete(w http.ResponseWriter, r *http.Request) {
	if err := h.repoSvc.Remove(r.PathValue("id")); err != nil {
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
	files, err := h.repoSvc.ListPluginFiles(r.Context(), id)
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
	repos, err := h.repoSvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type repoResult struct {
		repo  repoView
		files []storeFileView
		err   string
	}
	ch := make(chan repoResult, len(repos))
	for _, rec := range repos {
		go func(rec *pluginrepo.RepoRecord) {
			files, err := h.repoSvc.ListPluginFiles(r.Context(), rec.ID)
			if err != nil {
				ch <- repoResult{repo: toRepoView(rec), err: err.Error()}
				return
			}
			ch <- repoResult{repo: toRepoView(rec), files: h.describeFiles(r, rec.ID, files)}
		}(rec)
	}
	out := make([]map[string]any, 0, len(repos))
	for range repos {
		res := <-ch
		out = append(out, map[string]any{
			"repo": res.repo, "files": res.files, "error": res.err,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		a, _ := out[i]["repo"].(repoView)
		b, _ := out[j]["repo"].(repoView)
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
	ch := make(chan fileResult, len(files))
	for _, f := range files {
		go func(f pluginrepo.RepoFile) {
			view := storeFileView{RepoID: repoID, Path: f.Path}
			source, err := h.repoSvc.FetchFile(r.Context(), repoID, f.Path)
			if err != nil {
				view.Error = err.Error()
				ch <- fileResult{view: view}
				return
			}
			manifest, err := luaplugin.ParseManifest(source)
			if err != nil {
				view.Error = "manifest: " + err.Error()
				ch <- fileResult{view: view}
				return
			}
			view.DisplayName = manifest.Plugin
			view.Author = manifest.Author
			view.Version = manifest.Version
			view.Description = manifest.Description
			view.AllowHosts = manifest.AllowHosts
			view.Unsafe = manifest.Unsafe
			if id, err := luaplugin.BuildID(luaplugin.PluginOrigin{RepoID: repoID, Path: f.Path}, manifest); err == nil {
				if existing, err := h.luaSvc.Get(id); err == nil && existing != nil {
					view.Installed = true
					view.InstalledVersion = existing.Version
					if cmp, cerr := luaplugin.CompareVersions(manifest.Version, existing.Version); cerr == nil && cmp > 0 {
						view.UpdateAvailable = true
					}
				}
			}
			ch <- fileResult{view: view}
		}(f)
	}
	out := make([]storeFileView, 0, len(files))
	for range files {
		out = append(out, (<-ch).view)
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
	source, err := h.repoSvc.FetchFile(r.Context(), body.RepoID, body.Path)
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rec, err := h.luaSvc.Install(source, luaplugin.PluginOrigin{RepoID: body.RepoID, Path: body.Path})
	if err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.json(w, http.StatusOK, toPluginView(rec))
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
	installed, err := h.luaSvc.List()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type updateView struct {
		PluginID         string `json:"plugin_id"`
		Current          string `json:"current"`
		Latest           string `json:"latest"`
		RepoID           string `json:"repo_id"`
		Path             string `json:"path"`
		UpdateAvailable  bool   `json:"update_available"`
		Error            string `json:"error,omitempty"`
	}
	ch := make(chan updateView, len(installed))
	count := 0
	for _, rec := range installed {
		if rec.Origin.Manual || rec.Origin.RepoID == "" {
			continue
		}
		if rec.Origin.RepoID == "bundled" {
			continue
		}
		count++
		go func(rec *luaplugin.PluginRecord) {
			view := updateView{PluginID: rec.ID, Current: rec.Version, RepoID: rec.Origin.RepoID, Path: rec.Origin.Path}
			source, err := h.repoSvc.FetchFile(r.Context(), rec.Origin.RepoID, rec.Origin.Path)
			if err != nil {
				view.Error = err.Error()
				ch <- view
				return
			}
			manifest, err := luaplugin.ParseManifest(source)
			if err != nil {
				view.Error = err.Error()
				ch <- view
				return
			}
			view.Latest = manifest.Version
			if cmp, cerr := luaplugin.CompareVersions(manifest.Version, rec.Version); cerr == nil && cmp > 0 {
				view.UpdateAvailable = true
			}
			ch <- view
		}(rec)
	}
	out := make([]updateView, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, <-ch)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PluginID < out[j].PluginID })
	h.json(w, http.StatusOK, map[string]any{"updates": out})
}
