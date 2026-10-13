package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/services/doctor"
)

// apiDoctorInspect scans the database for orphaned data and corruption.
// @Summary      Inspect database health
// @Tags         Doctor
// @Produce      json
// @Success      200 {object} doctor.InspectionReport
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/doctor/inspect [get]
func (h *Handler) apiDoctorInspect(w http.ResponseWriter, r *http.Request) {
	report, err := h.doctorGormService.Inspect(r.Context())
	if err != nil {
		h.logError("Failed to inspect database", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.json(w, http.StatusOK, report)
}

// apiDoctorFix resolves database issues.
// @Summary      Fix database issues
// @Tags         Doctor
// @Accept       json
// @Produce      json
// @Param        payload body object{categories=[]string} false "Categories to fix (empty = all)"
// @Success      200 {object} object{fixed=int}
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/doctor/fix [post]
func (h *Handler) apiDoctorFix(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Categories []doctor.IssueCategory `json:"categories"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}

	fixed, err := h.doctorGormService.Fix(r.Context(), body.Categories)
	if err != nil {
		h.logError("Failed to repair database", "error", err)
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logInfo("Database repair completed", "count", fixed)
	h.json(w, http.StatusOK, map[string]any{"fixed": fixed})
}
