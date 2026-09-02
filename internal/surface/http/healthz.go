package http

import "net/http"

// healthz answers whether the process is up and serving requests — a
// liveness check, not a readiness one: it makes no application-layer
// call and touches no database, so it stays a simple presentation-only
// handler with nothing to decode and nothing to route between (the "call
// exactly one app method" rule doesn't bind it, the same way it doesn't
// bind internal/surface/cli's --help).
func (h *handlers) healthz(w http.ResponseWriter, _ *http.Request) {
	respond(w, http.StatusOK, healthzView{Status: "ok"})
}

type healthzView struct {
	Status string `json:"status" enum:"health_status"`
}
