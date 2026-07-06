package gateway

import (
	"fmt"
	"testing"
	"time"
)

func TestListSessionsAppliesGatewayFilters(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now()
	store.Set("s1", &session{
		Info:         SessionInfo{ID: "s1", Profile: "cpu", CreatedAt: now},
		managed:      true,
		experimentID: "exp-a",
		History:      NewStepHistory(),
	})
	store.Set("s2", &session{
		Info:         SessionInfo{ID: "s2", Profile: "gpu", CreatedAt: now},
		managed:      true,
		experimentID: "exp-a",
		History:      NewStepHistory(),
	})
	store.Set("s3", &session{
		Info:         SessionInfo{ID: "s3", Profile: "cpu", CreatedAt: now},
		managed:      true,
		experimentID: "exp-b",
		History:      NewStepHistory(),
	})
	gw := New(nil, nil, nil, nil, nil, GatewayConfig{}, store)

	got := gw.ListSessions(SessionListOptions{Profile: "cpu", ExperimentID: "exp-a"})
	if len(got) != 1 {
		t.Fatalf("filtered sessions length = %d, want 1: %#v", len(got), got)
	}
	if got[0].ID != "s1" {
		t.Fatalf("filtered session ID = %q, want s1", got[0].ID)
	}
}

func TestListSessionsPageAppliesStatusLimitAndCursor(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now()
	store.Set("s1", &session{
		Info:    SessionInfo{ID: "s1", Profile: "cpu", Status: "active", CreatedAt: now},
		History: NewStepHistory(),
	})
	store.Set("s2", &session{
		Info:    SessionInfo{ID: "s2", Profile: "cpu", Status: "active", CreatedAt: now.Add(time.Second)},
		History: NewStepHistory(),
	})
	store.Set("s3", &session{
		Info:    SessionInfo{ID: "s3", Profile: "cpu", Status: "active", CreatedAt: now.Add(2 * time.Second)},
		History: NewStepHistory(),
	})
	store.Set("s4", &session{
		Info:    SessionInfo{ID: "s4", Profile: "cpu", Status: "deleted", CreatedAt: now.Add(3 * time.Second)},
		History: NewStepHistory(),
	})
	gw := New(nil, nil, nil, nil, nil, GatewayConfig{}, store)

	page := gw.ListSessionsPage(SessionListOptions{Profile: "cpu", Status: "active", Limit: 1, Cursor: "s1"})
	if len(page.Items) != 1 {
		t.Fatalf("page length = %d, want 1: %#v", len(page.Items), page.Items)
	}
	if page.Items[0].ID != "s2" {
		t.Fatalf("page item = %q, want s2", page.Items[0].ID)
	}
	if page.NextCursor != "s2" {
		t.Fatalf("NextCursor = %q, want s2", page.NextCursor)
	}
}

func BenchmarkListSessionsIndexedExperiment(b *testing.B) {
	gw := benchmarkGatewayWithSessions(b, 10000)
	opts := SessionListOptions{
		ExperimentID: "exp-042",
		Status:       "active",
		Limit:        50,
	}
	b.ReportAllocs()
	for b.Loop() {
		page := gw.ListSessionsPage(opts)
		if len(page.Items) != 50 {
			b.Fatalf("page length = %d, want 50", len(page.Items))
		}
	}
}

func BenchmarkListSessionsIndexedProfile(b *testing.B) {
	gw := benchmarkGatewayWithSessions(b, 10000)
	opts := SessionListOptions{
		Profile: "cpu",
		Status:  "active",
		Limit:   100,
	}
	b.ReportAllocs()
	for b.Loop() {
		page := gw.ListSessionsPage(opts)
		if len(page.Items) != 100 {
			b.Fatalf("page length = %d, want 100", len(page.Items))
		}
	}
}

func benchmarkGatewayWithSessions(b *testing.B, count int) *Gateway {
	b.Helper()
	store := NewMemoryStore()
	now := time.Now().Add(-time.Hour)
	for i := range count {
		id := fmt.Sprintf("s-%05d", i)
		profile := "cpu"
		if i%3 == 0 {
			profile = "gpu"
		}
		store.Set(id, &session{
			Info: SessionInfo{
				ID:        id,
				Profile:   profile,
				Status:    "active",
				CreatedAt: now.Add(time.Duration(i) * time.Millisecond),
			},
			managed:      true,
			experimentID: fmt.Sprintf("exp-%03d", i%100),
			History:      NewStepHistory(),
		})
	}
	store.SetCount(int64(count))
	return New(nil, nil, nil, nil, nil, GatewayConfig{}, store)
}
