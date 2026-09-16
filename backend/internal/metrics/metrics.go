// Package metrics defines the backend's Prometheus metrics (planner
// task G-26). Registered against the default registry via promauto —
// the standard Go/Prometheus idiom, and why these are package-level
// vars rather than constructor-injected like this backend's other
// dependencies (audit.Logger, auth.TokenIssuer, ...): every real-world
// Prometheus client_golang service does it this way, since metrics are
// inherently process-global state, not per-request or per-connection.
// Served at /metrics via promhttp.Handler() in cmd/server/main.go, and
// scraped by the dev stack's Prometheus (infra/docker/prometheus.yml).
package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nexdesk_http_requests_total",
		Help: "Total HTTP requests, by path and status code.",
	}, []string{"path", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "nexdesk_http_request_duration_seconds",
		Help:    "HTTP request duration in seconds, by path.",
		Buckets: prometheus.DefBuckets,
	}, []string{"path"})

	// outcome: succeeded, failed, rate_limited, two_factor_required,
	// two_factor_failed — mirrors the audit event types api.go already
	// logs at these exact decision points (audit.EventLogin*).
	LoginAttemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "nexdesk_login_attempts_total",
		Help: "Login attempts, by outcome.",
	}, []string{"outcome"})

	RelaySessionsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nexdesk_relay_sessions_active",
		Help: "Currently paired relay sessions actively forwarding bytes.",
	})

	RelayPairingsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nexdesk_relay_pairings_total",
		Help: "Total relay stream pairs successfully matched.",
	})

	// Not split by direction: the relay pairs two arbitrary streams by
	// token and has no concept of which side is "host" or "client" (see
	// relay.go's own docs), so a direction label would be misleading
	// rather than informative.
	RelayBytesForwardedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nexdesk_relay_bytes_forwarded_total",
		Help: "Total bytes forwarded through the relay.",
	})
)

// Middleware records HTTPRequestsTotal/HTTPRequestDuration for every
// request. Labeled by the raw URL path directly rather than a matched
// route pattern: every path this backend serves is already static (no
// path parameters anywhere in cmd/server/main.go's mux), so the raw
// path is already low-cardinality — no template-matching step needed.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		HTTPRequestsTotal.WithLabelValues(r.URL.Path, strconv.Itoa(rec.status)).Inc()
		HTTPRequestDuration.WithLabelValues(r.URL.Path).Observe(time.Since(start).Seconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
