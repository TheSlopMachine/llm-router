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
// @Param        subsystem path string true "Subsystem name (providers, virtual_models, plugins, proxies, tokens)"
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
	case "proxies":
		data, err = h.dataSvc.ExportProxies()
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
	case "proxies":
		var proxies []*datamanagement.ProxyImport
		if err := decoder.Decode(&proxies); err != nil {
			h.jsonErr(w, http.StatusBadRequest, "invalid proxies payload")
			return
		}
		if err := h.dataSvc.ImportProxies(proxies); err != nil {
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
	case "proxies":
		err = h.dataSvc.ClearProxies()
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
