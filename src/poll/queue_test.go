package poll

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/store"
)

func TestQueueReceiptResolutionStates(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		expiresAt  time.Time
		wantBuilds int
	}{
		{name: "executable", status: http.StatusOK, body: `{"executable":{"number":42}}`, wantBuilds: 1},
		{name: "canceled", status: http.StatusOK, body: `{"cancelled":true}`},
		{name: "deleted", status: http.StatusGone},
		{name: "expired", status: http.StatusOK, body: `{}`, expiresAt: time.Now().Add(-time.Minute)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			runStore := store.NewForServer(t.TempDir(), "server-a")
			defer func() { _ = runStore.Close() }()
			manager := NewManager(gojenkins.CreateJenkins(nil, server.URL), runStore, Config{})
			defer manager.Stop()
			queueID := int64(7)
			expiresAt := tt.expiresAt
			if expiresAt.IsZero() {
				expiresAt = time.Now().Add(time.Hour)
			}
			intent := TriggerIntent{IntentID: tt.name, JobURL: server.URL + "/job/demo/", QueueID: &queueID, State: TriggerAccepted, CreatedAt: time.Now(), AcceptedAt: time.Now(), ExpiresAt: expiresAt}
			if err := manager.Track(context.Background(), intent); err != nil {
				t.Fatal(err)
			}
			if err := manager.reconcileTriggers(context.Background(), intent.JobURL); err != nil {
				t.Fatal(err)
			}
			if unresolved, err := runStore.UnresolvedTriggerIntents(context.Background()); err != nil || len(unresolved) != 0 {
				t.Fatalf("unresolved = %#v, %v", unresolved, err)
			}
			runs, err := runStore.GetAll()
			if err != nil {
				t.Fatal(err)
			}
			if len(runs) != tt.wantBuilds {
				t.Fatalf("runs = %#v, want %d", runs, tt.wantBuilds)
			}
			if tt.wantBuilds == 1 && (runs[0].BuildID != "42" || runs[0].Status != "QUEUED") {
				t.Fatalf("resolved run = %#v", runs[0])
			}
		})
	}
}
