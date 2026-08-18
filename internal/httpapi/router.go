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

	"github.com/jeanmamelo/medication-api/docs"
	"github.com/jeanmamelo/medication-api/internal/httpapi/swaggerui"
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
	mux.HandleFunc("GET /openapi.yaml", openAPIHandler)
	mux.Handle("GET /docs/", http.StripPrefix("/docs/", http.FileServerFS(swaggerui.FS)))
	if medications != nil {
		mux.HandleFunc("POST /v1/medications", createMedicationHandler(medications, logger))
		mux.HandleFunc("GET /v1/medications", listMedicationsHandler(medications, logger))
		mux.HandleFunc("GET /v1/medications/{id}", getMedicationHandler(medications, logger))
		mux.HandleFunc("PATCH /v1/medications/{id}", updateMedicationHandler(medications, logger))
		mux.HandleFunc("DELETE /v1/medications/{id}", deleteMedicationHandler(medications, logger))
	}

	return withRequestID(requestObservability(logger, metrics, securityHeaders(mux)))
}

// responseKey bounds metric cardinality: the route is the ServeMux pattern rather than
// the raw path, and unrecognized methods collapse into a single series.
type responseKey struct {
	method string
	route  string
	status int
}

// Metrics contains in-process HTTP counters suitable for scraping by a local collector.
type Metrics struct {
	requests  atomic.Uint64
	mu        sync.RWMutex
	responses map[responseKey]uint64
}

func NewMetrics() *Metrics {
	return &Metrics{responses: make(map[responseKey]uint64)}
}

func (metrics *Metrics) observe(key responseKey) {
	metrics.requests.Add(1)
	metrics.mu.Lock()
	metrics.responses[key]++
	metrics.mu.Unlock()
}

func (metrics *Metrics) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	metrics.mu.RLock()
	responses := make(map[responseKey]uint64, len(metrics.responses))
	for key, count := range metrics.responses {
		responses[key] = count
	}
	metrics.mu.RUnlock()

	keys := make([]responseKey, 0, len(responses))
	for key := range responses {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool {
		if keys[left].method != keys[right].method {
			return keys[left].method < keys[right].method
		}
		if keys[left].route != keys[right].route {
			return keys[left].route < keys[right].route
		}
		return keys[left].status < keys[right].status
	})

	writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = fmt.Fprintf(writer, "# HELP medication_http_requests_total Total HTTP requests.\n# TYPE medication_http_requests_total counter\nmedication_http_requests_total %d\n", metrics.requests.Load())
	for _, key := range keys {
		_, _ = fmt.Fprintf(writer, "medication_http_responses_total{method=\"%s\",route=\"%s\",status=\"%d\"} %d\n", escapeLabel(key.method), escapeLabel(key.route), key.status, responses[key])
	}
}

var knownMethods = map[string]bool{
	http.MethodGet: true, http.MethodPost: true, http.MethodPut: true,
	http.MethodPatch: true, http.MethodDelete: true, http.MethodHead: true,
	http.MethodOptions: true,
}

func responseLabels(request *http.Request, status int) responseKey {
	method, route := request.Method, request.Pattern
	if !knownMethods[method] {
		method = "other"
	}
	if route == "" {
		route = "unmatched"
	}
	return responseKey{method: method, route: route, status: status}
}

func escapeLabel(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
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
		metrics.observe(responseLabels(request, status))
		logger.Info("http request", "method", request.Method, "route", request.Pattern, "status", status, "duration", time.Since(started), "request_id", RequestIDFromContext(request.Context()))
	})
}

type medicationRequest struct {
	Name   *string `json:"name"`
	Dosage *string `json:"dosage"`
	Form   *string `json:"form"`
}

func createMedicationHandler(service MedicationService, logger *slog.Logger) http.HandlerFunc {
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
			writeServiceError(writer, request, logger, err)
			return
		}
		writer.Header().Set("Location", "/v1/medications/"+item.ID)
		writeJSON(writer, http.StatusCreated, item)
	}
}

func listMedicationsHandler(service MedicationService, logger *slog.Logger) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		limit, offset, ok := pagination(writer, request)
		if !ok {
			return
		}
		items, err := service.List(request.Context(), limit, offset)
		if err != nil {
			writeServiceError(writer, request, logger, err)
			return
		}
		writeJSON(writer, http.StatusOK, map[string]any{"items": items, "limit": limit, "offset": offset})
	}
}

func getMedicationHandler(service MedicationService, logger *slog.Logger) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		item, err := service.Get(request.Context(), request.PathValue("id"))
		if err != nil {
			writeServiceError(writer, request, logger, err)
			return
		}
		writeJSON(writer, http.StatusOK, item)
	}
}

func updateMedicationHandler(service MedicationService, logger *slog.Logger) http.HandlerFunc {
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
			writeServiceError(writer, request, logger, err)
			return
		}
		writeJSON(writer, http.StatusOK, item)
	}
}

func deleteMedicationHandler(service MedicationService, logger *slog.Logger) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if err := service.Delete(request.Context(), request.PathValue("id")); err != nil && !errors.Is(err, medication.ErrNotFound) {
			writeServiceError(writer, request, logger, err)
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
	limit, offset := medication.DefaultPageSize, 0
	var err error
	if value := request.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err == nil && request.URL.Query().Get("offset") != "" {
		offset, err = strconv.Atoi(request.URL.Query().Get("offset"))
	}
	if err != nil || medication.ValidatePagination(limit, offset) != nil {
		writeError(writer, http.StatusBadRequest, "invalid_pagination", fmt.Sprintf("limit must be 1 to %d and offset must be non-negative", medication.MaxPageSize))
		return 0, 0, false
	}
	return limit, offset, true
}

// writeServiceError maps domain errors to responses. Internal failures are logged here,
// at the point where the underlying error would otherwise be discarded.
func writeServiceError(writer http.ResponseWriter, request *http.Request, logger *slog.Logger, err error) {
	var validation *medication.ValidationError
	switch {
	case errors.Is(err, medication.ErrNotFound):
		writeError(writer, http.StatusNotFound, "not_found", "medication not found")
	case errors.Is(err, medication.ErrConflict):
		writeError(writer, http.StatusConflict, "conflict", "medication already exists")
	case errors.Is(err, medication.ErrInvalidPagination):
		writeError(writer, http.StatusBadRequest, "invalid_pagination", "invalid pagination")
	case errors.As(err, &validation):
		writeError(writer, http.StatusBadRequest, "validation_failed", validation.Error())
	default:
		logger.ErrorContext(request.Context(), "unhandled service error",
			"error", err,
			"method", request.Method,
			"route", request.Pattern,
			"request_id", RequestIDFromContext(request.Context()))
		writeError(writer, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}

func writeError(writer http.ResponseWriter, status int, code, message string) {
	writeJSON(writer, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func openAPIHandler(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = writer.Write(docs.OpenAPISpec)
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
		if strings.HasPrefix(request.URL.Path, "/docs/") {
			// Swagger UI needs to load its own same-origin scripts/styles and fetch the
			// spec; every other route keeps the fully locked-down default below.
			writer.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'")
		} else {
			writer.Header().Set("Content-Security-Policy", "default-src 'none'")
		}
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
