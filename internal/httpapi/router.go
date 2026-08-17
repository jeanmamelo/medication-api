// Package httpapi exposes the application's HTTP transport.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeanmamelo/medication-api/internal/medication"
)

// Readiness reports whether dependencies required to serve traffic are available.
// It keeps HTTP handlers independent from a specific database implementation.
type Readiness interface {
	Ready(context.Context) error
}

// AlwaysReady is used until external dependencies are composed into the application.
type AlwaysReady struct{}

// Ready implements Readiness.
func (AlwaysReady) Ready(context.Context) error {
	return nil
}

// NewRouter composes the public HTTP surface. Cross-cutting HTTP protections are
// applied once at the edge instead of being repeated by each handler.
type MedicationService interface {
	Create(context.Context, medication.CreateInput) (medication.Medication, error)
	Get(context.Context, string) (medication.Medication, error)
	List(context.Context, int, int) ([]medication.Medication, error)
	Update(context.Context, string, medication.UpdateInput) (medication.Medication, error)
	Delete(context.Context, string) error
}

func NewRouter(readiness Readiness, medications MedicationService) http.Handler {
	return NewRouterWithLogger(readiness, medications, slog.Default())
}

// NewRouterWithLogger composes the public HTTP surface with request logging and metrics.
func NewRouterWithLogger(readiness Readiness, medications MedicationService, logger *slog.Logger) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	metrics := NewMetrics()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthHandler)
	mux.HandleFunc("GET /readyz", readinessHandler(readiness))
	mux.Handle("GET /metrics", metrics)
	if medications != nil {
		mux.HandleFunc("POST /v1/medications", createMedicationHandler(medications))
		mux.HandleFunc("GET /v1/medications", listMedicationsHandler(medications))
		mux.HandleFunc("GET /v1/medications/{id}", getMedicationHandler(medications))
		mux.HandleFunc("PATCH /v1/medications/{id}", updateMedicationHandler(medications))
		mux.HandleFunc("DELETE /v1/medications/{id}", deleteMedicationHandler(medications))
	}

	return requestObservability(logger, metrics, securityHeaders(mux))
}

// Metrics contains in-process HTTP counters suitable for scraping by a local collector.
type Metrics struct {
	requests atomic.Uint64
	mu       sync.RWMutex
	statuses map[int]uint64
}

func NewMetrics() *Metrics {
	return &Metrics{statuses: make(map[int]uint64)}
}

func (metrics *Metrics) observe(status int) {
	metrics.requests.Add(1)
	metrics.mu.Lock()
	metrics.statuses[status]++
	metrics.mu.Unlock()
}

func (metrics *Metrics) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	metrics.mu.RLock()
	statuses := make(map[int]uint64, len(metrics.statuses))
	for status, count := range metrics.statuses {
		statuses[status] = count
	}
	metrics.mu.RUnlock()

	keys := make([]int, 0, len(statuses))
	for status := range statuses {
		keys = append(keys, status)
	}
	sort.Ints(keys)

	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(writer, "# HELP medication_http_requests_total Total HTTP requests.\n# TYPE medication_http_requests_total counter\nmedication_http_requests_total %d\n", metrics.requests.Load())
	for _, status := range keys {
		_, _ = fmt.Fprintf(writer, "medication_http_responses_total{status=\"%d\"} %d\n", status, statuses[status])
	}
}

type observabilityWriter struct {
	http.ResponseWriter
	status int
}

func (writer *observabilityWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func (writer *observabilityWriter) WriteHeader(status int) {
	if writer.status != 0 {
		return
	}
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *observabilityWriter) Write(bytes []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(bytes)
}

func requestObservability(logger *slog.Logger, metrics *Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		observed := &observabilityWriter{ResponseWriter: writer}
		next.ServeHTTP(observed, request)
		status := observed.status
		if status == 0 {
			status = http.StatusOK
		}
		metrics.observe(status)
		logger.Info("http request", "method", request.Method, "route", request.Pattern, "status", status, "duration", time.Since(started))
	})
}

type medicationRequest struct {
	Name   *string `json:"name"`
	Dosage *string `json:"dosage"`
	Form   *string `json:"form"`
}

func createMedicationHandler(service MedicationService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		input, ok := decodeMedicationRequest(writer, request)
		if !ok || input.Name == nil || input.Dosage == nil || input.Form == nil {
			if ok {
				writeError(writer, http.StatusBadRequest, "validation_failed", "name, dosage, and form are required")
			}
			return
		}
		item, err := service.Create(request.Context(), medication.CreateInput{Name: *input.Name, Dosage: *input.Dosage, Form: *input.Form})
		if err != nil {
			writeServiceError(writer, err)
			return
		}
		writer.Header().Set("Location", "/v1/medications/"+item.ID)
		writeJSON(writer, http.StatusCreated, item)
	}
}

func listMedicationsHandler(service MedicationService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		limit, offset, ok := pagination(writer, request)
		if !ok {
			return
		}
		items, err := service.List(request.Context(), limit, offset)
		if err != nil {
			writeServiceError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
	}
}

func getMedicationHandler(service MedicationService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		item, err := service.Get(request.Context(), request.PathValue("id"))
		if err != nil {
			writeServiceError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, item)
	}
}

func updateMedicationHandler(service MedicationService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		input, ok := decodeMedicationRequest(writer, request)
		if !ok {
			return
		}
		if input.Name == nil && input.Dosage == nil && input.Form == nil {
			writeError(writer, http.StatusBadRequest, "validation_failed", "at least one field is required")
			return
		}
		item, err := service.Update(request.Context(), request.PathValue("id"), medication.UpdateInput{Name: input.Name, Dosage: input.Dosage, Form: input.Form})
		if err != nil {
			writeServiceError(writer, err)
			return
		}
		writeJSON(writer, http.StatusOK, item)
	}
}

func deleteMedicationHandler(service MedicationService) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := service.Delete(request.Context(), request.PathValue("id")); err != nil {
			writeServiceError(writer, err)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}
}

func decodeMedicationRequest(writer http.ResponseWriter, request *http.Request) (medicationRequest, bool) {
	defer request.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var input medicationRequest
	if err := decoder.Decode(&input); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return medicationRequest{}, false
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		writeError(writer, http.StatusBadRequest, "invalid_request", "request body must contain one JSON object")
		return medicationRequest{}, false
	}
	return input, true
}

func pagination(writer http.ResponseWriter, request *http.Request) (int, int, bool) {
	limit, offset := 20, 0
	var err error
	if value := request.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err == nil && request.URL.Query().Get("offset") != "" {
		offset, err = strconv.Atoi(request.URL.Query().Get("offset"))
	}
	if err != nil || limit < 1 || limit > 100 || offset < 0 {
		writeError(writer, http.StatusBadRequest, "invalid_pagination", "limit must be 1 to 100 and offset must be non-negative")
		return 0, 0, false
	}
	return limit, offset, true
}

func writeServiceError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, medication.ErrNotFound):
		writeError(writer, http.StatusNotFound, "not_found", "medication not found")
	case errors.Is(err, medication.ErrInvalidPagination):
		writeError(writer, http.StatusBadRequest, "invalid_pagination", "invalid pagination")
	case strings.Contains(err.Error(), " is required"):
		writeError(writer, http.StatusBadRequest, "validation_failed", err.Error())
	default:
		writeError(writer, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func healthHandler(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func readinessHandler(readiness Readiness) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := readiness.Ready(request.Context()); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}

		writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Security-Policy", "default-src 'none'")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("X-Frame-Options", "DENY")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("Cache-Control", "no-store")

		next.ServeHTTP(writer, request)
	})
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}
