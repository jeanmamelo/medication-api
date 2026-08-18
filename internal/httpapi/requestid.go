package httpapi

import (
	"context"
	"net/http"

	"github.com/jeanmamelo/medication-api/internal/medication"
)

// RequestIDHeader carries the correlation ID between the client, the access log, and
// any error logged while serving the request.
const RequestIDHeader = "X-Request-Id"

const maxRequestIDLength = 64

type requestIDKey struct{}

var requestIDGenerator medication.IDGenerator = medication.RandomUUIDGenerator{}

// RequestIDFromContext returns the correlation ID assigned to the request, or an empty
// string when the request did not pass through withRequestID.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// withRequestID reuses a caller-supplied ID when it is safe to echo back, and generates
// one otherwise, so every request is traceable end to end.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		id := sanitizeRequestID(request.Header.Get(RequestIDHeader))
		if id == "" {
			generated, err := requestIDGenerator.New()
			if err != nil {
				generated = "unknown"
			}
			id = generated
		}

		writer.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(writer, request.WithContext(context.WithValue(request.Context(), requestIDKey{}, id)))
	})
}

// sanitizeRequestID accepts only bounded, printable ASCII so an inbound header cannot
// inject control characters into logs or response headers.
func sanitizeRequestID(value string) string {
	if value == "" || len(value) > maxRequestIDLength {
		return ""
	}
	for _, character := range []byte(value) {
		if character < 0x21 || character > 0x7e {
			return ""
		}
	}
	return value
}
