package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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

type medicationServiceStub struct{ item medication.Medication }

func (stub *medicationServiceStub) Create(_ context.Context, _ medication.CreateInput) (medication.Medication, error) {
	return stub.item, nil
}
func (stub *medicationServiceStub) Get(_ context.Context, _ string) (medication.Medication, error) {
	return stub.item, nil
}
func (stub *medicationServiceStub) List(_ context.Context, _, _ int) ([]medication.Medication, error) {
	return []medication.Medication{stub.item}, nil
}
func (stub *medicationServiceStub) Update(_ context.Context, _ string, _ medication.UpdateInput) (medication.Medication, error) {
	return stub.item, nil
}
func (stub *medicationServiceStub) Delete(_ context.Context, _ string) error { return nil }
