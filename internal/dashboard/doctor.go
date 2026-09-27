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
	report, err := h.doctorSvc.Inspect()
	if err != nil {
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

	fixed, err := h.doctorSvc.Fix(body.Categories)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.logger.Info("database doctor fix executed", "fixed_count", fixed)
	h.json(w, http.StatusOK, map[string]any{"fixed": fixed})
}
