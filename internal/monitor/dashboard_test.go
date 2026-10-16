package monitor

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRegisterRoutesProtectsAPIsButServesLoginShell(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	health := NewPollingHealth(130 * time.Second)
	RegisterRoutes(r, NewMetrics(), "secret", nil, nil, health)

	for _, tc := range []struct {
		path string
		auth string
		want int
	}{
		{"/api/metrics", "", http.StatusUnauthorized},
		{"/api/metrics", "Bearer wrong", http.StatusUnauthorized},
		{"/api/metrics", "Bearer secret", http.StatusOK},
		{"/dashboard", "", http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("Authorization", tc.auth)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Fatalf("%s with %q: got %d, want %d", tc.path, tc.auth, w.Code, tc.want)
		}
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/dashboard", nil))
	body := w.Body.String()
	for _, want := range []string{"id=\"token\"", "Authorization", "Bearer ", "login"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard does not contain %q", want)
		}
	}
	if strings.Contains(body, "cdn.tailwindcss.com") || strings.Contains(body, "localStorage") || strings.Contains(body, "document.cookie") {
		t.Error("dashboard must not expose an in-memory admin token to third-party code or persistent storage")
	}
}

func TestRegisterRoutesLeavesAPIsPublicWithoutAdminToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterRoutes(r, NewMetrics(), "", nil, nil, NewPollingHealth(130*time.Second))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestHealthTracksPollingFreshness(t *testing.T) {
	gin.SetMode(gin.TestMode)
	health := NewPollingHealth(130 * time.Second)
	r := gin.New()
	RegisterRoutes(r, NewMetrics(), "", nil, nil, health)

	status := func() int {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
		return w.Code
	}
	if got := status(); got != http.StatusServiceUnavailable {
		t.Fatalf("before first poll: got %d, want %d", got, http.StatusServiceUnavailable)
	}
	now := time.Now()
	health.RecordSuccess(now)
	if got := health.Status(now.Add(129 * time.Second)); got != PollHealthy {
		t.Fatalf("short outage should stay healthy, got %s", got)
	}
	if got := health.Status(now.Add(130 * time.Second)); got != PollStale {
		t.Fatalf("130 seconds without a successful poll should be stale, got %s", got)
	}
	health.RecordSuccess(time.Now().Add(-131 * time.Second))
	if got := status(); got != http.StatusServiceUnavailable {
		t.Fatalf("stale polling: got %d, want %d", got, http.StatusServiceUnavailable)
	}
	health.RecordSuccess(time.Now()) // empty getUpdates is a valid recovery.
	if got := status(); got != http.StatusOK {
		t.Fatalf("successful poll should recover health endpoint, got %d", got)
	}
}
