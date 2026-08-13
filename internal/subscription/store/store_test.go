package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLinkedInstancesMigrationAndRefresh(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "subscriptions.db")
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`
CREATE TABLE subscriptions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    slug TEXT UNIQUE NOT NULL,
    name TEXT NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE TABLE instances (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    raw_uri TEXT NOT NULL,
    label TEXT,
    created_at DATETIME NOT NULL
);`)
	if err != nil {
		_ = legacy.Close()
		t.Fatal(err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() migration error = %v", err)
	}
	defer func() { _ = s.Close() }()

	if !hasColumn(t, s.db, "instances", "source_instance_id") {
		t.Fatal("legacy instances table was not migrated")
	}
	for _, sub := range []string{"beta", "alpha"} {
		if _, err := s.CreateSubscription(sub, sub); err != nil {
			t.Fatal(err)
		}
	}

	manualURI := "olcrtc://manual@room/manual?key=one#manual"
	oldMainURI := "olcrtc://wbstream@room/old-main?key=two#main"
	newMainURI := "olcrtc://wbstream@room/new-main?key=two&auth_token=fresh#main"
	secondaryURI := "olcrtc://wbstream@room/secondary?key=three#secondary"
	mainID, secondaryID, negativeID := 0, 2, -1

	if _, err := s.AddInstance("alpha", manualURI); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddInstanceWithSource("alpha", oldMainURI, &mainID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddInstanceWithSource("beta", oldMainURI, &mainID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddInstanceWithSource("beta", secondaryURI, &secondaryID); err != nil {
		t.Fatal(err)
	}
	negative, err := s.AddInstanceWithSource("alpha", "olcrtc://manual@room/negative?key=four#negative", &negativeID)
	if err != nil {
		t.Fatal(err)
	}
	if negative.SourceInstanceID != nil {
		t.Fatalf("negative source ID was retained: %v", *negative.SourceInstanceID)
	}

	changed, err := s.RefreshLinkedInstances(map[int]string{
		mainID:      newMainURI,
		secondaryID: secondaryURI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"alpha", "beta"}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed subscriptions = %v, want %v", changed, want)
	}

	alpha, err := s.ListInstances("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if alpha[0].RawURI != manualURI {
		t.Fatalf("manual URI changed to %q", alpha[0].RawURI)
	}
	if alpha[1].SourceInstanceID == nil || *alpha[1].SourceInstanceID != 0 || alpha[1].RawURI != newMainURI {
		t.Fatalf("main linked instance was not refreshed: %+v", alpha[1])
	}

	exported, err := s.Export()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Open(filepath.Join(t.TempDir(), "restored.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = restored.Close() }()
	if _, _, err := restored.Import(exported, false); err != nil {
		t.Fatal(err)
	}
	restoredAlpha, err := restored.ListInstances("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if restoredAlpha[1].SourceInstanceID == nil || *restoredAlpha[1].SourceInstanceID != 0 {
		t.Fatalf("linked source ID was lost during export/import: %+v", restoredAlpha[1])
	}

	changed, err = s.RefreshLinkedInstances(map[int]string{mainID: newMainURI})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed) != 0 {
		t.Fatalf("unchanged URI reported subscriptions: %v", changed)
	}
}

func TestDeleteInstancesBySource(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "subscriptions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	for _, slug := range []string{"alpha", "beta"} {
		if _, err := s.CreateSubscription(slug, slug); err != nil {
			t.Fatal(err)
		}
	}
	sourceID := 0
	for _, slug := range []string{"alpha", "beta"} {
		if _, err := s.AddInstanceWithSource(slug, "olcrtc://wbstream@room/"+slug+"?key=one#linked", &sourceID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddInstance("alpha", "olcrtc://manual@room/keep?key=two#manual"); err != nil {
		t.Fatal(err)
	}

	slugs, removed, err := s.DeleteInstancesBySource(sourceID)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 2 || !reflect.DeepEqual(slugs, []string{"alpha", "beta"}) {
		t.Fatalf("DeleteInstancesBySource() removed=%d slugs=%v", removed, slugs)
	}
	alpha, err := s.ListInstances("alpha")
	if err != nil {
		t.Fatal(err)
	}
	if len(alpha) != 1 || alpha[0].SourceInstanceID != nil {
		t.Fatalf("alpha instances after delete = %+v", alpha)
	}
	beta, err := s.ListInstances("beta")
	if err != nil {
		t.Fatal(err)
	}
	if len(beta) != 0 {
		t.Fatalf("beta instances after delete = %+v", beta)
	}
}

func hasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

func TestEnsureCoreParam(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want string
	}{
		{
			"adds core to bare uri",
			"olcrtc://wbstream@r/room?k=abc&c=cli",
			"olcrtc://wbstream@r/room?k=abc&c=cli&core=legacy",
		},
		{
			"adds core before fragment",
			"olcrtc://wbstream@r/room?k=abc&c=cli#room%20name",
			"olcrtc://wbstream@r/room?k=abc&c=cli&core=legacy#room%20name",
		},
		{
			"adds core when no query yet",
			"olcrtc://jitsi@r/room#name",
			"olcrtc://jitsi@r/room?core=legacy#name",
		},
		{
			"preserves already-encoded values",
			"olcrtc://wbstream@r/room?k=ab%20cd&c=cli",
			"olcrtc://wbstream@r/room?k=ab%20cd&c=cli&core=legacy",
		},
		{
			"does not duplicate existing core",
			"olcrtc://wbstream@r/room?k=abc&core=current",
			"olcrtc://wbstream@r/room?k=abc&core=current",
		},
		{
			"preserves existing core with fragment",
			"olcrtc://wbstream@r/room?core=legacy&c=cli#x",
			"olcrtc://wbstream@r/room?core=legacy&c=cli#x",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ensureCoreParam(tt.uri); got != tt.want {
				t.Errorf("ensureCoreParam(%q) = %q, want %q", tt.uri, got, tt.want)
			}
		})
	}
}
