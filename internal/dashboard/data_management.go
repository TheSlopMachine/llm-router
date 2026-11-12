package dashboard

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
	"github.com/TheSlopMachine/llm-router/internal/services/datamanagement"
)

// apiDataStats returns subsystem counts.
// @Summary      Get subsystem stats
// @Tags         Data
// @Produce      json
// @Success      200 {object} datamanagement.SubsystemStats
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/data/stats [get]
func (h *Handler) apiDataStats(w http.ResponseWriter, r *http.Request) {
	statistics, err := h.dataGormService.Stats(r.Context())
	if err != nil {
		h.logError("Failed to read subsystem statistics", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, statistics)
}

// maxPluginsArchiveBytes caps the plugins import body.
const maxPluginsArchiveBytes = 64 << 20

// decodeOneOrMany decodes a JSON array of T or a single T object (a one-item
// export file) into a slice.
func decodeOneOrMany[T any](body io.Reader) ([]*T, error) {
	var raw json.RawMessage
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var item T
		if err := json.Unmarshal(trimmed, &item); err != nil {
			return nil, err
		}
		return []*T{&item}, nil
	}
	var items []*T
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// apiDataExport export a subsystem.
// @Summary      Export subsystem data
// @Description  Returns the subsystem as JSON. The plugins subsystem is a zip archive: plugins.json (repositories and plugin records without source and history) plus one <plugin id>.lua per plugin.
// @Tags         Data
// @Produce      json,application/zip
// @Param        subsystem path string true "Subsystem name (providers, virtual_models, plugins, tokens, proxy)"
// @Success      200 {object} object
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/data/{subsystem}/export [get]
func (h *Handler) apiDataExport(w http.ResponseWriter, r *http.Request) {
	subsystem := r.PathValue("subsystem")
	var data any
	var err error

	switch subsystem {
	case "providers":
		data, err = h.dataGormService.ExportProviders(r.Context())
	case "virtual_models":
		data, err = h.dataGormService.ExportVirtualModels(r.Context())
	case "plugins":
		h.exportPluginsArchive(w, r)
		return
	case "tokens":
		data, err = h.dataGormService.ExportTokens(r.Context())
	case "proxy":
		data, err = h.dataGormService.ExportProxy(r.Context())
	default:
		h.jsonErr(w, http.StatusBadRequest, "unknown subsystem")
		return
	}

	if err != nil {
		h.logError("Failed to export subsystem data", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, data)
}

// exportPluginsArchive answers the plugins export with the zip archive. The
// archive is built in memory first, so a failure still answers JSON.
func (h *Handler) exportPluginsArchive(w http.ResponseWriter, r *http.Request) {
	bundle, err := h.dataGormService.ExportPlugins(r.Context())
	if err != nil {
		h.logError("Failed to export subsystem data", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var archive bytes.Buffer
	if err := datamanagement.WritePluginsArchive(&archive, bundle); err != nil {
		h.logError("Failed to export subsystem data", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="llm_router_plugins_export.zip"`)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(archive.Bytes()); err != nil {
		h.logError("Failed to write plugins archive", "error", err)
	}
}

// apiDataImport imports data into a subsystem.
// @Summary      Import subsystem data
// @Description  Takes the subsystem export. providers and virtual_models also take one item as a JSON object (the per-item export). plugins takes the zip archive of the plugins export as the raw body.
// @Tags         Data
// @Accept       json,application/zip
// @Produce      json
// @Param        subsystem path string true "Subsystem name"
// @Param        payload body object true "Import payload"
// @Success      200 {object} object{ok=bool}
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/data/{subsystem}/import [post]
func (h *Handler) apiDataImport(w http.ResponseWriter, r *http.Request) {
	subsystem := r.PathValue("subsystem")
	decoder := json.NewDecoder(r.Body)

	switch subsystem {
	case "providers":
		bundles, err := decodeOneOrMany[datamanagement.ProviderBundle](r.Body)
		if err != nil {
			h.jsonErr(w, http.StatusBadRequest, "invalid providers bundle payload")
			return
		}
		if err := h.dataGormService.ImportProviders(r.Context(), bundles); err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	case "virtual_models":
		virtualModels, err := decodeOneOrMany[datamanagement.VirtualModelImport](r.Body)
		if err != nil {
			h.jsonErr(w, http.StatusBadRequest, "invalid virtual models payload")
			return
		}
		if err := h.dataGormService.ImportVirtualModels(r.Context(), virtualModels); err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	case "plugins":
		archive, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPluginsArchiveBytes))
		if err != nil {
			h.jsonErr(w, http.StatusBadRequest, "read plugins archive: "+err.Error())
			return
		}
		bundle, err := datamanagement.ReadPluginsArchive(archive)
		if err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := h.dataGormService.ImportPlugins(r.Context(), bundle); err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	case "proxy":
		var bundle models.ProxyDataBundle
		if err := decoder.Decode(&bundle); err != nil {
			h.jsonErr(w, http.StatusBadRequest, "invalid proxy data payload")
			return
		}
		if err := h.dataGormService.ImportProxy(r.Context(), &bundle); err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	case "tokens":
		var tokens []*datamanagement.TokenImport
		if err := decoder.Decode(&tokens); err != nil {
			h.jsonErr(w, http.StatusBadRequest, "invalid tokens payload")
			return
		}
		if err := h.dataGormService.ImportTokens(r.Context(), tokens); err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		h.jsonErr(w, http.StatusBadRequest, "unknown subsystem")
		return
	}

	h.logInfo("Subsystem data imported", "subsystem", subsystem)
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiDataClear truncates a subsystem.
// @Summary      Clear subsystem data
// @Tags         Data
// @Produce      json
// @Param        subsystem path string true "Subsystem name"
// @Success      200 {object} object{ok=bool}
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/data/{subsystem}/clear [post]
func (h *Handler) apiDataClear(w http.ResponseWriter, r *http.Request) {
	subsystem := r.PathValue("subsystem")
	var err error

	switch subsystem {
	case "providers":
		err = h.dataGormService.ClearProviders(r.Context())
	case "virtual_models":
		err = h.dataGormService.ClearVirtualModels(r.Context())
	case "plugins":
		err = h.dataGormService.ClearPlugins(r.Context())
	case "tokens":
		err = h.dataGormService.ClearTokens(r.Context())
	case "proxy":
		err = h.dataGormService.ClearProxy(r.Context())
	default:
		h.jsonErr(w, http.StatusBadRequest, "unknown subsystem")
		return
	}

	if err != nil {
		h.logError("Failed to clear subsystem data", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logInfo("Subsystem data cleared", "subsystem", subsystem)
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiProviderExport exports a single provider with its credentials and overrides.
// @Summary      Export provider data
// @Tags         Data
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {object} datamanagement.ProviderBundle
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/export [get]
func (h *Handler) apiProviderExport(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	bundle, err := h.dataGormService.ExportProvider(r.Context(), id)
	if err != nil {
		h.jsonErr(w, http.StatusNotFound, err.Error())
		return
	}
	h.json(w, http.StatusOK, bundle)
}

// apiProviderImport imports a single provider bundle.
// @Summary      Import provider data
// @Tags         Data
// @Accept       json
// @Produce      json
// @Param        payload body datamanagement.ProviderBundle true "Provider bundle payload"
// @Success      200 {object} object{ok=bool}
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/import [post]
func (h *Handler) apiProviderImport(w http.ResponseWriter, r *http.Request) {
	var bundle datamanagement.ProviderBundle
	if err := json.NewDecoder(r.Body).Decode(&bundle); err != nil {
		h.jsonErr(w, http.StatusBadRequest, "invalid provider bundle payload")
		return
	}
	if err := h.dataGormService.ImportProvider(r.Context(), &bundle); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logInfo("Provider data imported", "provider_id", bundle.Instance.ID)
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiProviderPurge removes all data of a provider. A plugin-backed provider
// stays with default settings; a custom provider is removed.
// @Summary      Purge provider data
// @Description  Removes the settings, credentials, model overrides, model cache and managed virtual models of a provider. A plugin-backed provider stays with default settings; a custom provider is removed.
// @Tags         Data
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {object} object{ok=bool}
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/purge [delete]
func (h *Handler) apiProviderPurge(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.dataGormService.PurgeProvider(r.Context(), id); err != nil {
		h.logError("Failed to purge provider", "provider_id", id, "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.logInfo("Provider purged", "provider_id", id)
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}
