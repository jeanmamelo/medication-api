package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jeanmamelo/medication-api/internal/medication"
)

func TestHealthz(t *testing.T) {
	response := httptest.NewRecorder()
	NewRouter(AlwaysReady{}, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if body := response.Body.String(); body != "{\"status\":\"ok\"}\n" {
		t.Errorf("body = %q", body)
	}
	assertSecurityHeaders(t, response)
}

func TestReadyz(t *testing.T) {
	tests := []struct {
		name       string
		readiness  Readiness
		wantStatus int
	}{
		{name: "ready", readiness: AlwaysReady{}, wantStatus: http.StatusOK},
		{name: "dependency unavailable", readiness: readinessFunc(func(context.Context) error { return errors.New("database unavailable") }), wantStatus: http.StatusServiceUnavailable},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewRouter(test.readiness, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
		})
	}
}

func TestOpenAPISpec(t *testing.T) {
	response := httptest.NewRecorder()
	NewRouter(AlwaysReady{}, nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Content-Type"); got != "application/yaml; charset=utf-8" {
		t.Errorf("content type = %q", got)
	}
	if body := response.Body.String(); !strings.Contains(body, "openapi: 3.1.0") || !strings.Contains(body, "/v1/medications") {
		t.Errorf("body missing expected spec content: %q", body)
	}
	assertSecurityHeaders(t, response)
}

func TestSwaggerUIServesDocs(t *testing.T) {
	router := NewRouter(AlwaysReady{}, nil)

	index := httptest.NewRecorder()
	router.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/docs/", nil))
	if index.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", index.Code, http.StatusOK)
	}
	if body := index.Body.String(); !strings.Contains(body, "swagger-ui-bundle.js") {
		t.Errorf("index body missing swagger-ui-bundle.js reference: %q", body)
	}
	if got, want := index.Header().Get("Content-Security-Policy"), "default-src 'self'; img-src 'self' data:; font-src 'self' data:; connect-src 'self'"; got != want {
		t.Errorf("docs CSP = %q, want %q", got, want)
	}

	redirect := httptest.NewRecorder()
	router.ServeHTTP(redirect, httptest.NewRequest(http.MethodGet, "/docs", nil))
	if redirect.Code != http.StatusTemporaryRedirect {
		t.Errorf("status = %d, want %d", redirect.Code, http.StatusTemporaryRedirect)
	}
	if got := redirect.Header().Get("Location"); got != "/docs/" {
		t.Errorf("Location = %q, want %q", got, "/docs/")
	}

	asset := httptest.NewRecorder()
	router.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, "/docs/swagger-ui-bundle.js", nil))
	if asset.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", asset.Code, http.StatusOK)
	}

	strictElsewhere := httptest.NewRecorder()
	router.ServeHTTP(strictElsewhere, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got, want := strictElsewhere.Header().Get("Content-Security-Policy"), "default-src 'none'"; got != want {
		t.Errorf("non-docs CSP = %q, want %q", got, want)
	}
}

func TestMedicationEndpoints(t *testing.T) {
	item := medication.Medication{ID: "id", Name: "Paracetamol", Dosage: "500 mg", Form: "tablet"}
	service := medicationServiceStub{item: item}
	router := NewRouter(AlwaysReady{}, &service)

	create := httptest.NewRecorder()
	router.ServeHTTP(create, httptest.NewRequest(http.MethodPost, "/v1/medications", bytes.NewBufferString(`{"name":" Paracetamol ","dosage":"500 mg","form":"tablet"}`)))
	if create.Code != http.StatusCreated || create.Header().Get("Location") != "/v1/medications/id" {
		t.Fatalf("create status/location = %d/%q", create.Code, create.Header().Get("Location"))
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/medications?limit=1&offset=0", nil))
	if list.Code != http.StatusOK || list.Body.String() != `{"items":[{"id":"id","name":"Paracetamol","dosage":"500 mg","form":"tablet"}],"limit":1,"offset":0}`+"\n" {
		t.Fatalf("list = %d %q", list.Code, list.Body.String())
	}

	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/v1/medications", bytes.NewBufferString(`{}`)))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d", invalid.Code)
	}
}

func TestMedicationReadUpdateDeleteEndpoints(t *testing.T) {
	item := medication.Medication{ID: "id", Name: "Paracetamol", Dosage: "500 mg", Form: "tablet"}
	service := &medicationServiceStub{item: item}
	router := NewRouter(AlwaysReady{}, service)

	response := perform(router, http.MethodGet, "/v1/medications/id", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"id"`) {
		t.Fatalf("GET status/body = %d/%q", response.Code, response.Body.String())
	}

	response = perform(router, http.MethodPatch, "/v1/medications/id", `{"dosage":"750 mg"}`)
	if response.Code != http.StatusOK || service.updateID != "id" || service.updateInput.Dosage == nil || *service.updateInput.Dosage != "750 mg" {
		t.Fatalf("PATCH status/input = %d/%#v", response.Code, service.updateInput)
	}

	response = perform(router, http.MethodDelete, "/v1/medications/id", "")
	if response.Code != http.StatusNoContent || service.deleteID != "id" {
		t.Fatalf("DELETE status/id = %d/%q", response.Code, service.deleteID)
	}
}

func TestDeleteMedicationReturnsNoContentWhenAlreadyGone(t *testing.T) {
	service := &medicationServiceStub{deleteErr: medication.ErrNotFound}
	router := NewRouter(AlwaysReady{}, service)

	response := perform(router, http.MethodDelete, "/v1/medications/missing", "")
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("DELETE status/body = %d/%q", response.Code, response.Body.String())
	}
}

func TestMedicationEndpointsMapServiceErrors(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		setErr func(*medicationServiceStub)
		want   int
	}{
		{name: "get not found", method: http.MethodGet, path: "/v1/medications/id", setErr: func(s *medicationServiceStub) { s.getErr = medication.ErrNotFound }, want: http.StatusNotFound},
		{name: "get internal", method: http.MethodGet, path: "/v1/medications/id", setErr: func(s *medicationServiceStub) { s.getErr = errors.New("database failed") }, want: http.StatusInternalServerError},
		{name: "create validation", method: http.MethodPost, path: "/v1/medications", body: `{"name":"x","dosage":"y","form":"z"}`, setErr: func(s *medicationServiceStub) { s.createErr = &medication.ValidationError{Field: "name"} }, want: http.StatusBadRequest},
		{name: "create wrapped validation", method: http.MethodPost, path: "/v1/medications", body: `{"name":"x","dosage":"y","form":"z"}`, setErr: func(s *medicationServiceStub) {
			s.createErr = fmt.Errorf("create medication: %w", &medication.ValidationError{Field: "dosage"})
		}, want: http.StatusBadRequest},
		{name: "create internal", method: http.MethodPost, path: "/v1/medications", body: `{"name":"x","dosage":"y","form":"z"}`, setErr: func(s *medicationServiceStub) { s.createErr = errors.New("database failed") }, want: http.StatusInternalServerError},
		{name: "create conflict", method: http.MethodPost, path: "/v1/medications", body: `{"name":"x","dosage":"y","form":"z"}`, setErr: func(s *medicationServiceStub) { s.createErr = medication.ErrConflict }, want: http.StatusConflict},
		{name: "list invalid pagination", method: http.MethodGet, path: "/v1/medications", setErr: func(s *medicationServiceStub) { s.listErr = medication.ErrInvalidPagination }, want: http.StatusBadRequest},
		{name: "list internal", method: http.MethodGet, path: "/v1/medications", setErr: func(s *medicationServiceStub) { s.listErr = errors.New("database failed") }, want: http.StatusInternalServerError},
		{name: "update not found", method: http.MethodPatch, path: "/v1/medications/id", body: `{"name":"x"}`, setErr: func(s *medicationServiceStub) { s.updateErr = medication.ErrNotFound }, want: http.StatusNotFound},
		{name: "update internal", method: http.MethodPatch, path: "/v1/medications/id", body: `{"name":"x"}`, setErr: func(s *medicationServiceStub) { s.updateErr = errors.New("database failed") }, want: http.StatusInternalServerError},
		{name: "delete internal", method: http.MethodDelete, path: "/v1/medications/id", setErr: func(s *medicationServiceStub) { s.deleteErr = errors.New("database failed") }, want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &medicationServiceStub{item: medication.Medication{ID: "id"}}
			test.setErr(service)
			response := perform(NewRouter(AlwaysReady{}, service), test.method, test.path, test.body)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d; body = %q", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestMedicationRequestValidationAndPagination(t *testing.T) {
	service := &medicationServiceStub{item: medication.Medication{ID: "id"}}
	router := NewRouter(AlwaysReady{}, service)
	for _, test := range []struct {
		name, method, path, body string
	}{
		{name: "malformed json", method: http.MethodPost, path: "/v1/medications", body: "{"},
		{name: "unknown field", method: http.MethodPost, path: "/v1/medications", body: `{"name":"x","dosage":"y","form":"z","extra":true}`},
		{name: "multiple objects", method: http.MethodPost, path: "/v1/medications", body: `{"name":"x","dosage":"y","form":"z"}{}`},
		{name: "empty patch", method: http.MethodPatch, path: "/v1/medications/id", body: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := perform(router, test.method, test.path, test.body)
			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400", response.Code)
			}
		})
	}
	for _, query := range []string{"?limit=0", "?limit=101", "?limit=-1", "?offset=-1", "?limit=nope", "?offset=nope"} {
		response := perform(router, http.MethodGet, "/v1/medications"+query, "")
		if response.Code != http.StatusBadRequest {
			t.Errorf("query %s status = %d, want 400", query, response.Code)
		}
	}
	if service.createCalls != 0 || service.updateCalls != 0 || service.listCalls != 0 {
		t.Errorf("invalid requests reached service: create=%d update=%d list=%d", service.createCalls, service.updateCalls, service.listCalls)
	}
}

func TestValidationErrorMessageExcludesWrapperContext(t *testing.T) {
	service := &medicationServiceStub{createErr: fmt.Errorf("create medication: %w", &medication.ValidationError{Field: "dosage"})}
	response := perform(NewRouter(AlwaysReady{}, service), http.MethodPost, "/v1/medications", `{"name":"x","dosage":"y","form":"z"}`)

	if body := response.Body.String(); !strings.Contains(body, `"message":"dosage is required"`) {
		t.Fatalf("body = %q", body)
	}
}

func TestMetricsAndRequestLogs(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	service := &medicationServiceStub{item: medication.Medication{ID: "secret-id"}}
	router := NewRouterWithLogger(AlwaysReady{}, service, logger)

	perform(router, http.MethodGet, "/v1/medications/secret-id", "")
	metrics := perform(router, http.MethodGet, "/metrics", "")
	if metrics.Code != http.StatusOK || !strings.Contains(metrics.Body.String(), "medication_http_requests_total 1") {
		t.Fatalf("metrics = %d %q", metrics.Code, metrics.Body.String())
	}
	if want := `medication_http_responses_total{method="GET",route="GET /v1/medications/{id}",status="200"} 1`; !strings.Contains(metrics.Body.String(), want) {
		t.Errorf("metrics missing labelled series %q: %q", want, metrics.Body.String())
	}
	if got := metrics.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Errorf("metrics content type = %q", got)
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, `route="GET /v1/medications/{id}"`) || !strings.Contains(logOutput, "status=200") {
		t.Errorf("log does not contain route/status: %q", logOutput)
	}
	if strings.Contains(logOutput, "secret-id") {
		t.Errorf("log exposed medication ID: %q", logOutput)
	}
}

func TestMetricsCollapseUnmatchedRequests(t *testing.T) {
	router := NewRouter(AlwaysReady{}, &medicationServiceStub{})
	perform(router, "BREW", "/nowhere", "")

	metrics := perform(router, http.MethodGet, "/metrics", "")
	if want := `medication_http_responses_total{method="other",route="unmatched",status="404"} 1`; !strings.Contains(metrics.Body.String(), want) {
		t.Errorf("metrics missing %q: %q", want, metrics.Body.String())
	}
}

func TestInternalErrorsAreLoggedWithRequestID(t *testing.T) {
	var logs strings.Builder
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))
	service := &medicationServiceStub{getErr: errors.New("connection refused by database")}
	router := NewRouterWithLogger(AlwaysReady{}, service, logger)

	request := httptest.NewRequest(http.MethodGet, "/v1/medications/id", nil)
	request.Header.Set(RequestIDHeader, "trace-me")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", response.Code)
	}
	if body := response.Body.String(); strings.Contains(body, "connection refused") {
		t.Errorf("response leaked the underlying error: %q", body)
	}
	logOutput := logs.String()
	for _, want := range []string{"unhandled service error", "connection refused by database", "request_id=trace-me", "level=ERROR"} {
		if !strings.Contains(logOutput, want) {
			t.Errorf("log missing %q: %q", want, logOutput)
		}
	}
}

func TestRequestIDIsEchoedAndGenerated(t *testing.T) {
	router := NewRouter(AlwaysReady{}, &medicationServiceStub{})

	for _, test := range []struct {
		name, supplied string
		wantEcho       bool
	}{
		{name: "reuses safe inbound id", supplied: "abc-123", wantEcho: true},
		{name: "replaces oversized id", supplied: strings.Repeat("x", maxRequestIDLength+1)},
		{name: "replaces id with control characters", supplied: "bad\nid"},
		{name: "generates when absent"},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			if test.supplied != "" {
				request.Header.Set(RequestIDHeader, test.supplied)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)

			got := response.Header().Get(RequestIDHeader)
			if got == "" {
				t.Fatal("response has no request ID")
			}
			if test.wantEcho && got != test.supplied {
				t.Errorf("request ID = %q, want %q", got, test.supplied)
			}
			if !test.wantEcho && got == test.supplied {
				t.Errorf("unsafe request ID %q was echoed back", got)
			}
		})
	}
}

func TestRequestIDReachesHandlerContext(t *testing.T) {
	var seen string
	handler := withRequestID(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		seen = RequestIDFromContext(request.Context())
	}))

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set(RequestIDHeader, "trace-me")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if seen != "trace-me" {
		t.Fatalf("RequestIDFromContext() = %q, want %q", seen, "trace-me")
	}
}

func perform(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(method, path, bytes.NewBufferString(body)))
	return response
}

func assertSecurityHeaders(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()

	for key, want := range map[string]string{
		"Content-Security-Policy": "default-src 'none'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
		"Cache-Control":           "no-store",
	} {
		if got := response.Header().Get(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
}

type readinessFunc func(context.Context) error

func (function readinessFunc) Ready(ctx context.Context) error {
	return function(ctx)
}

type medicationServiceStub struct {
	item                                             medication.Medication
	createErr, getErr, listErr, updateErr, deleteErr error
	createCalls, listCalls, updateCalls              int
	updateID, deleteID                               string
	updateInput                                      medication.UpdateInput
}

func (stub *medicationServiceStub) Create(_ context.Context, _ medication.CreateInput) (medication.Medication, error) {
	stub.createCalls++
	return stub.item, stub.createErr
}
func (stub *medicationServiceStub) Get(_ context.Context, _ string) (medication.Medication, error) {
	return stub.item, stub.getErr
}
func (stub *medicationServiceStub) List(_ context.Context, _, _ int) ([]medication.Medication, error) {
	stub.listCalls++
	return []medication.Medication{stub.item}, stub.listErr
}
func (stub *medicationServiceStub) Update(_ context.Context, id string, input medication.UpdateInput) (medication.Medication, error) {
	stub.updateCalls++
	stub.updateID, stub.updateInput = id, input
	return stub.item, stub.updateErr
}
func (stub *medicationServiceStub) Delete(_ context.Context, id string) error {
	stub.deleteID = id
	return stub.deleteErr
}
