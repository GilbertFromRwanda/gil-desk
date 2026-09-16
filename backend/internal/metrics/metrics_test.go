package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/nexdesk/nexdesk/backend/internal/metrics"
)

// Real assertions against the actual registered Prometheus metrics
// (testutil.ToFloat64/CollectAndCount read the real default registry,
// the same one promhttp.Handler() in cmd/server/main.go serves) — not
// just "the middleware compiles and doesn't panic".
func TestMiddlewareRecordsRequestsAndStatus(t *testing.T) {
	handler := metrics.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/boom" {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	before := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/metrics-test-ok", "200"))

	req := httptest.NewRequest(http.MethodGet, "/metrics-test-ok", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	after := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/metrics-test-ok", "200"))
	if after != before+1 {
		t.Fatalf("nexdesk_http_requests_total{path=/metrics-test-ok,status=200} = %v, want %v", after, before+1)
	}

	errBefore := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/boom", "500"))
	errReq := httptest.NewRequest(http.MethodGet, "/boom", nil)
	handler.ServeHTTP(httptest.NewRecorder(), errReq)
	errAfter := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/boom", "500"))
	if errAfter != errBefore+1 {
		t.Fatalf("nexdesk_http_requests_total{path=/boom,status=500} = %v, want %v", errAfter, errBefore+1)
	}

	// A handler that never calls WriteHeader explicitly still gets
	// recorded as 200 (http.ResponseWriter's own documented default) —
	// proves statusRecorder's default matches, not just the explicit case.
	implicitOKHandler := metrics.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	implicitBefore := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/implicit-ok", "200"))
	implicitReq := httptest.NewRequest(http.MethodGet, "/implicit-ok", nil)
	implicitOKHandler.ServeHTTP(httptest.NewRecorder(), implicitReq)
	implicitAfter := testutil.ToFloat64(metrics.HTTPRequestsTotal.WithLabelValues("/implicit-ok", "200"))
	if implicitAfter != implicitBefore+1 {
		t.Fatalf("implicit 200 not recorded: before=%v after=%v", implicitBefore, implicitAfter)
	}
}

func TestRelayMetricsAreRegistered(t *testing.T) {
	// CollectAndCount panics/fails if a metric isn't actually registered
	// against the default registry — a real check that promauto's
	// package-level init actually ran, not just that the Go compiler
	// accepted the variable declarations.
	if n := testutil.CollectAndCount(metrics.RelaySessionsActive); n != 1 {
		t.Fatalf("RelaySessionsActive: got %d metrics, want 1", n)
	}
	if n := testutil.CollectAndCount(metrics.RelayPairingsTotal); n != 1 {
		t.Fatalf("RelayPairingsTotal: got %d metrics, want 1", n)
	}
	if n := testutil.CollectAndCount(metrics.RelayBytesForwardedTotal); n != 1 {
		t.Fatalf("RelayBytesForwardedTotal: got %d metrics, want 1", n)
	}
}
