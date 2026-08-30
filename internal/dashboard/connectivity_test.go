package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func connectivitySnapshot(observedAt time.Time) Snapshot {
	return Snapshot{
		GeneratedAt: observedAt,
		Nodes: []Node{{
			NodeID: dashboardPrincipalID,
			Name:   "workstation",
			Kind:   "mac",
			Connectivity: &Connectivity{
				ObservedAt:          observedAt,
				SnapshotGeneration:  12,
				BundleGeneration:    7,
				RootGeneration:      3,
				UserGeneration:      2,
				Aggregate:           "degraded",
				Authorization:       "authorized",
				AuthorizationReason: "none",
				OpenGaps:            2,
				GapOverflow:         true,
				SourceConflicts:     1,
				AwaitingBaseline:    1,
				ConflictOverflow:    true,
				LineageReset:        true,
				Components: []ConnectivityComponent{
					{Name: "dns", State: "unknown", Freshness: "never_observed", DiffReason: "no_observation"},
					{Name: "relays", State: "degraded", Freshness: "fresh", DiffReason: "none"},
				},
				ProposalClasses: []ConnectivityProposalClass{{Class: "restore", Count: 2}},
			},
		}},
	}
}

func renderDashboard(t *testing.T, snapshot Snapshot, now time.Time) string {
	t.Helper()
	auth := &authFixture{authorized: true}
	store := &dashboardStoreFixture{snapshot: snapshot}
	pages, err := NewHandler(store, auth, func() time.Time { return now })
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	router, err := NewRouter(pages, auth)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("dashboard responded %d", response.Code)
	}
	return response.Body.String()
}

// The integrity signals are the reason this section exists. A page that shows
// an aggregate while hiding that holes were dropped, evidence was evicted or a
// lineage restarted would read as healthier than the host reported.
func TestConnectivitySectionShowsWhatTheHostReported(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	body := renderDashboard(t, connectivitySnapshot(now), now)

	for _, expected := range []string{
		"Connectivity read model",
		"observe-only",
		"degraded",
		"dns: unknown/never_observed",
		"relays: degraded/fresh",
		"restore: 2",
		"snapshot 12",
		"bundle 7",
		"dropped",
		"evicted",
		"awaiting baseline",
		"lineage reset",
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("the connectivity section does not show %q", expected)
		}
	}
}

// A node that never sent a projection has no row, and the section says so
// rather than looking like a node with nothing wrong.
func TestConnectivitySectionIsEmptyWithoutAProjection(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	snapshot := connectivitySnapshot(now)
	snapshot.Nodes[0].Connectivity = nil
	body := renderDashboard(t, snapshot, now)
	if !strings.Contains(body, "No connectivity projections") {
		t.Fatal("a node without a projection did not produce the empty state")
	}
}

// A stale projection is marked, not hidden.
func TestStaleProjectionIsShownAndMarked(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	snapshot := connectivitySnapshot(now.Add(-2 * time.Hour))
	snapshot.Nodes[0].Connectivity.Stale = true
	body := renderDashboard(t, snapshot, now)
	if !strings.Contains(body, "workstation") {
		t.Fatal("a stale projection was hidden instead of marked")
	}
	section := body[strings.Index(body, "Connectivity read model"):]
	section = section[:strings.Index(section, "</section>")]
	if !strings.Contains(section, `class="state bad"`) {
		t.Fatal("a stale projection was not marked")
	}
}

// The dashboard is a reader. No page may offer a way to send anything to a
// host, and the rendered markup is where such an offer would first appear.
func TestConnectivitySectionOffersNoControl(t *testing.T) {
	now := time.Date(2026, time.August, 30, 12, 0, 0, 0, time.UTC)
	body := renderDashboard(t, connectivitySnapshot(now), now)
	section := body[strings.Index(body, "Connectivity read model"):]
	section = section[:strings.Index(section, "</section>")]
	for _, forbidden := range []string{
		"<form", "<input", "method=\"post\"", "fetch(", "apply", "execute",
		"reconcile", "proposal_digest",
	} {
		if strings.Contains(strings.ToLower(section), forbidden) {
			t.Fatalf("the connectivity section offers %q", forbidden)
		}
	}
}
