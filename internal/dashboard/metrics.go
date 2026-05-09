package dashboard

import (
	"net/http"

	"github.com/TheSlopMachine/llm-router/internal/models"
)

// apiMetricsOverview returns aggregated metrics
// @Summary      Get metrics overview
// @Description  Returns aggregated API usage metrics for the specified time range and filters.
// @Tags         Metrics
// @Produce      json
// @Param        time_range query string false "Time range" Enums(hour, 1d, 7d, 28d, 90d, month) default(hour)
// @Param        provider_id query string false "Filter by provider ID"
// @Param        model query string false "Filter by model"
// @Success      200 {object} models.MetricsOverview
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/metrics/overview [get]
func (h *Handler) apiMetricsOverview(w http.ResponseWriter, r *http.Request) {
	filters := models.MetricsFilters{
		ProviderID: r.URL.Query().Get("provider_id"),
		Model:      models.ModelId(r.URL.Query().Get("model")),
		TimeRange:  models.TimeRange(r.URL.Query().Get("time_range")),
	}

	if filters.TimeRange == "" {
		filters.TimeRange = models.TimeRangeHour
	}

	overview, err := h.metricsSvc.QueryOverview(filters)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, overview)
}

// apiMetricsTimeSeries returns time-series data for charts
// @Summary      Get metrics time series
// @Description  Returns time-series data for a specific metric.
// @Tags         Metrics
// @Produce      json
// @Param        metric query string true "Metric name" Enums(requests, errors, tokens_input, tokens_output)
// @Param        time_range query string false "Time range" Enums(hour, 1d, 7d, 28d, 90d, month) default(hour)
// @Param        provider_id query string false "Filter by provider ID"
// @Param        model query string false "Filter by model"
// @Success      200 {array} models.TimeSeriesPoint
// @Failure      400 {object} models.ErrorResponse
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/metrics/timeseries [get]
func (h *Handler) apiMetricsTimeSeries(w http.ResponseWriter, r *http.Request) {
	metric := r.URL.Query().Get("metric")
	if metric == "" {
		h.jsonErr(w, http.StatusBadRequest, "metric parameter required")
		return
	}

	filters := models.MetricsFilters{
		ProviderID: r.URL.Query().Get("provider_id"),
		Model:      models.ModelId(r.URL.Query().Get("model")),
		TimeRange:  models.TimeRange(r.URL.Query().Get("time_range")),
	}

	if filters.TimeRange == "" {
		filters.TimeRange = models.TimeRangeHour
	}

	points, err := h.metricsSvc.QueryTimeSeries(metric, filters)
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, points)
}

// apiMetricsModels returns distinct models from metrics data
// @Summary      Get distinct models
// @Description  Returns list of models that have been used in API requests.
// @Tags         Metrics
// @Produce      json
// @Success      200 {array} string
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/metrics/models [get]
func (h *Handler) apiMetricsModels(w http.ResponseWriter, r *http.Request) {
	models, err := h.metricsSvc.GetDistinctModels()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, models)
}

// apiTokenUsage returns request counts for all tokens
// @Summary      Get token usage statistics
// @Description  Returns total request count for each token.
// @Tags         Tokens
// @Produce      json
// @Success      200 {object} map[string]int64
// @Failure      401 {object} models.ErrorResponse
// @Failure      500 {object} models.ErrorResponse
// @Security     SessionAuth
// @Router       /api/llm-router/dashboard/tokens/usage [get]
func (h *Handler) apiTokenUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := h.metricsSvc.GetTokenUsage()
	if err != nil {
		h.jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.json(w, http.StatusOK, usage)
}

