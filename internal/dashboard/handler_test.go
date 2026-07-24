package dashboard

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const dashboardPrincipalID = metadata.UUID("11111111-1111-4111-8111-111111111111")

func TestRouterProtectsDataAndHasNoControlSurface(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	auth := &authFixture{}
	store := &dashboardStoreFixture{snapshot: Snapshot{
		Nodes: []Node{{
			NodeID: dashboardPrincipalID,
			Name:   `<script>alert("x")</script>`,
			Kind:   "mac",
		}},
		Incidents: []Incident{{
			Category:       "availability",
			Component:      "tunnel",
			Severity:       "warning",
			Status:         "open",
			RequiresAction: true,
			Generation:     1,
			LastObservedAt: now,
		}},
	}}
	pages, err := NewHandler(store, auth, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	router, err := NewRouter(pages, auth)
	if err != nil {
		t.Fatalf("NewRouter() error = %v", err)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusSeeOther ||
		response.Header().Get("Location") != "/login" ||
		store.loads != 0 {
		t.Fatalf("unauthenticated dashboard = %d location=%q loads=%d", response.Code, response.Header().Get("Location"), store.loads)
	}

	auth.authorized = true
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "Hexroute") ||
		strings.Contains(response.Body.String(), `<script>alert`) ||
		!strings.Contains(response.Body.String(), "&lt;script&gt;") {
		t.Fatalf("dashboard response = %d %q", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "default-src 'none'") {
		t.Fatalf("CSP = %q", response.Header().Get("Content-Security-Policy"))
	}

	for _, path := range []string{
		"/control",
		"/restart",
		"/api/actions",
		"/api/routes",
		"/admin",
	} {
		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404", path, response.Code)
		}
	}
}

func TestDashboardFailureDoesNotExposeDatabaseDetail(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	auth := &authFixture{authorized: true}
	pages, err := NewHandler(
		&dashboardStoreFixture{err: errors.New("postgres host secret")},
		auth,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	pages.Dashboard(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusServiceUnavailable ||
		strings.Contains(response.Body.String(), "postgres") ||
		strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("failure response = %d %q", response.Code, response.Body.String())
	}
}

type authFixture struct {
	authorized bool
}

func (auth *authFixture) Authorize(*http.Request) (metadata.UUID, string, bool) {
	return dashboardPrincipalID, "operator", auth.authorized
}

func (*authFixture) BeginLogin(http.ResponseWriter, *http.Request)         {}
func (*authFixture) FinishLogin(http.ResponseWriter, *http.Request)        {}
func (*authFixture) BeginRegistration(http.ResponseWriter, *http.Request)  {}
func (*authFixture) FinishRegistration(http.ResponseWriter, *http.Request) {}
func (*authFixture) Logout(http.ResponseWriter, *http.Request)             {}

type dashboardStoreFixture struct {
	snapshot Snapshot
	err      error
	loads    int
}

func (store *dashboardStoreFixture) Load(
	context.Context,
	time.Time,
) (Snapshot, error) {
	store.loads++
	return store.snapshot, store.err
}
