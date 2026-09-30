package dashboard

import (
	"encoding/json"
	"net/http"

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
	st, err := h.dataSvc.Stats()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, st)
}

// apiDataExport export a subsystem.
// @Summary      Export subsystem data
// @Tags         Data
// @Produce      json
// @Param        subsystem path string true "Subsystem name (providers, virtual_models, plugins, tokens)"
// @Success      200 {object} object
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/data/{subsystem}/export [get]
func (h *Handler) apiDataExport(w http.ResponseWriter, r *http.Request) {
	sub := r.PathValue("subsystem")
	var data any
	var err error

	switch sub {
	case "providers":
		data, err = h.dataSvc.ExportProviders()
	case "virtual_models":
		data, err = h.dataSvc.ExportVirtualModels()
	case "plugins":
		data, err = h.dataSvc.ExportPlugins()
	case "tokens":
		data, err = h.dataSvc.ExportTokens()
	default:
		h.jsonErr(w, http.StatusBadRequest, "unknown subsystem")
		return
	}

	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, data)
}

// apiDataImport imports data into a subsystem.
// @Summary      Import subsystem data
// @Tags         Data
// @Accept       json
// @Produce      json
// @Param        subsystem path string true "Subsystem name"
// @Param        payload body object true "Import payload"
// @Success      200 {object} object{ok=bool}
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/data/{subsystem}/import [post]
func (h *Handler) apiDataImport(w http.ResponseWriter, r *http.Request) {
	sub := r.PathValue("subsystem")
	decoder := json.NewDecoder(r.Body)

	switch sub {
	case "providers":
		var bundles []*datamanagement.ProviderBundle
		if err := decoder.Decode(&bundles); err != nil {
			h.jsonErr(w, http.StatusBadRequest, "invalid providers bundle payload")
			return
		}
		if err := h.dataSvc.ImportProviders(bundles); err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	case "virtual_models":
		var vms []*datamanagement.VirtualModelImport
		if err := decoder.Decode(&vms); err != nil {
			// Try plain VirtualModel slice
			var plain []*datamanagement.VirtualModelImport
			if err := decoder.Decode(&plain); err != nil {
				h.jsonErr(w, http.StatusBadRequest, "invalid virtual models payload")
				return
			}
			vms = plain
		}
		if err := h.dataSvc.ImportVirtualModels(vms); err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	case "plugins":
		var bundle datamanagement.PluginsExportBundle
		if err := decoder.Decode(&bundle); err != nil {
			h.jsonErr(w, http.StatusBadRequest, "invalid plugins payload")
			return
		}
		if err := h.dataSvc.ImportPlugins(r.Context(), &bundle); err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	case "tokens":
		var tokens []*datamanagement.TokenImport
		if err := decoder.Decode(&tokens); err != nil {
			h.jsonErr(w, http.StatusBadRequest, "invalid tokens payload")
			return
		}
		if err := h.dataSvc.ImportTokens(tokens); err != nil {
			h.jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	default:
		h.jsonErr(w, http.StatusBadRequest, "unknown subsystem")
		return
	}

	h.logger.Info("subsystem data imported", "subsystem", sub)
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
	sub := r.PathValue("subsystem")
	var err error

	switch sub {
	case "providers":
		err = h.dataSvc.ClearProviders()
	case "virtual_models":
		err = h.dataSvc.ClearVirtualModels()
	case "plugins":
		err = h.dataSvc.ClearPlugins()
	case "tokens":
		err = h.dataSvc.ClearTokens()
	default:
		h.jsonErr(w, http.StatusBadRequest, "unknown subsystem")
		return
	}

	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logger.Info("subsystem data cleared", "subsystem", sub)
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
	bundle, err := h.dataSvc.ExportProvider(id)
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
	if err := h.dataSvc.ImportProvider(&bundle); err != nil {
		h.jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h.logger.Info("provider data imported", "provider_id", bundle.Instance.ID)
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}

// apiProviderPurge completely removes a provider and all associated data.
// @Summary      Purge provider completely
// @Tags         Data
// @Produce      json
// @Param        id path string true "Provider ID"
// @Success      200 {object} object{ok=bool}
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/providers/{id}/purge [delete]
func (h *Handler) apiProviderPurge(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.dataSvc.PurgeProvider(id); err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.logger.Info("provider completely purged", "provider_id", id)
	h.json(w, http.StatusOK, map[string]any{"ok": true})
}
