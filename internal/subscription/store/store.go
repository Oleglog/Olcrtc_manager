// Package store provides SQLite-backed CRUD operations for subscriptions and instances.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/subscription/model"

	// Pure-Go SQLite driver (no CGO required).
	_ "modernc.org/sqlite"
)

var (
	// ErrSlugExists is returned when a subscription with the given slug already exists.
	ErrSlugExists = errors.New("subscription slug already exists")
	// ErrNotFound is returned when the requested resource does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInvalidURI is returned when an olcrtc:// URI is malformed.
	ErrInvalidURI = errors.New("invalid olcrtc:// URI")
)

// Store wraps an SQLite database for subscription storage.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at the given path and
// initialises the schema.
func Open(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	// ponytail: subscription traffic is small; one connection keeps SQLite
	// pragmas deterministic while the legacy instance and Admin may share WAL.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set SQLite busy timeout: %w", err)
	}

	// WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := backfillCoreParam(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("backfill core param: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

func migrate(db *sql.DB) error {
	const schema = `
CREATE TABLE IF NOT EXISTS subscriptions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    slug       TEXT    UNIQUE NOT NULL,
    name       TEXT    NOT NULL,
    created_at DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS instances (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    subscription_id INTEGER NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
    source_instance_id INTEGER,
    raw_uri         TEXT    NOT NULL,
    label           TEXT,
    created_at      DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS subscription_mirrors (
    subscription_id INTEGER PRIMARY KEY REFERENCES subscriptions(id) ON DELETE CASCADE,
    type            TEXT NOT NULL,
    public_url      TEXT NOT NULL,
    key_b64         TEXT NOT NULL,
    updated_at      DATETIME NOT NULL
);
`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	if err := ensureInstanceSourceColumn(db); err != nil {
		return err
	}
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_instances_source_instance_id ON instances(source_instance_id)"); err != nil {
		return fmt.Errorf("create source instance index: %w", err)
	}
	// Enable FK enforcement.
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	return err
}

func ensureInstanceSourceColumn(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(instances)")
	if err != nil {
		return fmt.Errorf("inspect instances schema: %w", err)
	}
	defer func() { _ = rows.Close() }()

	hasColumn := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return fmt.Errorf("scan instances schema: %w", err)
		}
		if name == "source_instance_id" {
			hasColumn = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read instances schema: %w", err)
	}
	if hasColumn {
		return nil
	}
	if _, err := db.Exec("ALTER TABLE instances ADD COLUMN source_instance_id INTEGER"); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return nil
		}
		return fmt.Errorf("add source_instance_id: %w", err)
	}
	return nil
}

// ── Subscriptions ───────────────────────────────────────────────────────────

// CreateSubscription inserts a new subscription. Returns ErrSlugExists if
// the slug is already taken.
func (s *Store) CreateSubscription(slug, name string) (*model.Subscription, error) {
	now := time.Now().UTC()
	res, err := s.db.Exec(
		"INSERT INTO subscriptions (slug, name, created_at) VALUES (?, ?, ?)",
		slug, name, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint") {
			return nil, ErrSlugExists
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Subscription{ID: id, Slug: slug, Name: name, CreatedAt: now}, nil
}

// ListSubscriptions returns all subscriptions ordered by creation date.
func (s *Store) ListSubscriptions() ([]model.Subscription, error) {
	rows, err := s.db.Query(
		"SELECT id, slug, name, created_at FROM subscriptions ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	subs := make([]model.Subscription, 0)
	for rows.Next() {
		var sub model.Subscription
		if err := rows.Scan(&sub.ID, &sub.Slug, &sub.Name, &sub.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, sub)
	}
	return subs, rows.Err()
}

// GetSubscriptionBySlug returns a single subscription or ErrNotFound.
func (s *Store) GetSubscriptionBySlug(slug string) (*model.Subscription, error) {
	var sub model.Subscription
	err := s.db.QueryRow(
		"SELECT id, slug, name, created_at FROM subscriptions WHERE slug = ?", slug,
	).Scan(&sub.ID, &sub.Slug, &sub.Name, &sub.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sub, nil
}

// DeleteSubscription removes a subscription and all its instances.
func (s *Store) DeleteSubscription(slug string) error {
	res, err := s.db.Exec("DELETE FROM subscriptions WHERE slug = ?", slug)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// ── Instances ───────────────────────────────────────────────────────────────

// AddInstance adds an olcrtc:// URI to a subscription.
func (s *Store) AddInstance(slug, rawURI string) (*model.Instance, error) {
	return s.AddInstanceWithSource(slug, rawURI, nil)
}

// AddInstanceWithSource adds an olcrtc:// URI and optionally links it to an
// Admin UI instance. Instance ID zero is the main Admin UI instance.
func (s *Store) AddInstanceWithSource(slug, rawURI string, sourceInstanceID *int) (*model.Instance, error) {
	if !strings.HasPrefix(rawURI, "olcrtc://") {
		return nil, ErrInvalidURI
	}
	rawURI = ensureCoreParam(rawURI)

	sub, err := s.GetSubscriptionBySlug(slug)
	if err != nil {
		return nil, err
	}

	label := extractLabel(rawURI)
	now := time.Now().UTC()

	var source any
	var linkedSource *int
	if sourceInstanceID != nil && *sourceInstanceID >= 0 {
		value := *sourceInstanceID
		source = value
		linkedSource = &value
	}
	res, err := s.db.Exec(
		"INSERT INTO instances (subscription_id, source_instance_id, raw_uri, label, created_at) VALUES (?, ?, ?, ?, ?)",
		sub.ID, source, rawURI, label, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Instance{
		ID: id, SubscriptionID: sub.ID,
		SourceInstanceID: linkedSource, RawURI: rawURI, Label: label, CreatedAt: now,
	}, nil
}

// ListInstances returns all instances for a subscription.
func (s *Store) ListInstances(slug string) ([]model.Instance, error) {
	sub, err := s.GetSubscriptionBySlug(slug)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		"SELECT id, subscription_id, source_instance_id, raw_uri, label, created_at FROM instances WHERE subscription_id = ? ORDER BY created_at",
		sub.ID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var insts []model.Instance
	for rows.Next() {
		var inst model.Instance
		var source sql.NullInt64
		if err := rows.Scan(&inst.ID, &inst.SubscriptionID, &source, &inst.RawURI, &inst.Label, &inst.CreatedAt); err != nil {
			return nil, err
		}
		if source.Valid {
			value := int(source.Int64)
			inst.SourceInstanceID = &value
		}
		insts = append(insts, inst)
	}
	return insts, rows.Err()
}

// RefreshLinkedInstances replaces snapshot URIs for entries linked to Admin
// UI instances. It returns subscription slugs whose content changed.
func (s *Store) RefreshLinkedInstances(uris map[int]string) ([]string, error) {
	if len(uris) == 0 {
		return nil, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	changed := make(map[string]struct{})
	for sourceID, rawURI := range uris {
		if sourceID < 0 || !strings.HasPrefix(rawURI, "olcrtc://") {
			continue
		}
		rows, err := tx.Query(`SELECT DISTINCT s.slug
FROM instances i JOIN subscriptions s ON s.id = i.subscription_id
WHERE i.source_instance_id = ? AND i.raw_uri <> ?`, sourceID, rawURI)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var slug string
			if err := rows.Scan(&slug); err != nil {
				_ = rows.Close()
				return nil, err
			}
			changed[slug] = struct{}{}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
		if _, err := tx.Exec("UPDATE instances SET raw_uri = ?, label = ? WHERE source_instance_id = ? AND raw_uri <> ?", rawURI, extractLabel(rawURI), sourceID, rawURI); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	slugs := make([]string, 0, len(changed))
	for slug := range changed {
		slugs = append(slugs, slug)
	}
	sort.Strings(slugs)
	return slugs, nil
}

// DeleteInstancesBySource removes every subscription entry linked to an Admin
// UI instance and returns the affected subscription slugs.
func (s *Store) DeleteInstancesBySource(sourceID int) ([]string, int64, error) {
	if sourceID < 0 {
		return nil, 0, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	rows, err := tx.Query(`SELECT DISTINCT s.slug
FROM instances i JOIN subscriptions s ON s.id = i.subscription_id
WHERE i.source_instance_id = ?`, sourceID)
	if err != nil {
		return nil, 0, err
	}
	var slugs []string
	for rows.Next() {
		var slug string
		if err := rows.Scan(&slug); err != nil {
			_ = rows.Close()
			return nil, 0, err
		}
		slugs = append(slugs, slug)
	}
	if err := rows.Close(); err != nil {
		return nil, 0, err
	}

	result, err := tx.Exec("DELETE FROM instances WHERE source_instance_id = ?", sourceID)
	if err != nil {
		return nil, 0, err
	}
	removed, _ := result.RowsAffected()
	if err := tx.Commit(); err != nil {
		return nil, 0, err
	}
	sort.Strings(slugs)
	return slugs, removed, nil
}

// DeleteInstance removes a single instance by ID.
func (s *Store) DeleteInstance(id int64) error {
	res, err := s.db.Exec("DELETE FROM instances WHERE id = ?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DetachInstances removes all instances from a subscription without deleting the subscription.
func (s *Store) DetachInstances(slug string) (int64, error) {
	sub, err := s.GetSubscriptionBySlug(slug)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec("DELETE FROM instances WHERE subscription_id = ?", sub.ID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// InstanceURIs returns the raw URIs for a subscription (one per line, for
// the public GET /sub/{slug} endpoint).
func (s *Store) InstanceURIs(slug string) ([]string, error) {
	sub, err := s.GetSubscriptionBySlug(slug)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		"SELECT raw_uri FROM instances WHERE subscription_id = ? ORDER BY created_at",
		sub.ID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var uris []string
	for rows.Next() {
		var uri string
		if err := rows.Scan(&uri); err != nil {
			return nil, err
		}
		uris = append(uris, uri)
	}
	return uris, rows.Err()
}

// GetMirror returns mirror metadata for a subscription.
func (s *Store) GetMirror(slug string) (*model.Mirror, error) {
	sub, err := s.GetSubscriptionBySlug(slug)
	if err != nil {
		return nil, err
	}
	var m model.Mirror
	err = s.db.QueryRow("SELECT subscription_id, type, public_url, key_b64, updated_at FROM subscription_mirrors WHERE subscription_id = ?", sub.ID).Scan(&m.SubscriptionID, &m.Type, &m.URL, &m.Key, &m.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// GetOrCreateMirrorKey returns the existing mirror key or creates one with keyGen.
func (s *Store) GetOrCreateMirrorKey(slug string, keyGen func() (string, error)) (string, error) {
	sub, err := s.GetSubscriptionBySlug(slug)
	if err != nil {
		return "", err
	}
	var key string
	err = s.db.QueryRow("SELECT key_b64 FROM subscription_mirrors WHERE subscription_id = ?", sub.ID).Scan(&key)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	key, err = keyGen()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	_, err = s.db.Exec("INSERT INTO subscription_mirrors (subscription_id, type, public_url, key_b64, updated_at) VALUES (?, ?, ?, ?, ?)", sub.ID, "yandex_disk", "", key, now)
	if err != nil {
		return "", err
	}
	return key, nil
}

// UpsertMirror stores the latest public mirror URL.
func (s *Store) UpsertMirror(slug, typ, publicURL, key string) (*model.Mirror, error) {
	sub, err := s.GetSubscriptionBySlug(slug)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = s.db.Exec(`INSERT INTO subscription_mirrors (subscription_id, type, public_url, key_b64, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(subscription_id) DO UPDATE SET type = excluded.type, public_url = excluded.public_url, key_b64 = excluded.key_b64, updated_at = excluded.updated_at`, sub.ID, typ, publicURL, key, now)
	if err != nil {
		return nil, err
	}
	return &model.Mirror{SubscriptionID: sub.ID, Type: typ, URL: publicURL, Key: key, UpdatedAt: now}, nil
}

// ── Export / Import ─────────────────────────────────────────────────────────

// Export returns all subscriptions with their instances in export format.
func (s *Store) Export() (*model.ExportFormat, error) {
	subs, err := s.ListSubscriptions()
	if err != nil {
		return nil, err
	}

	exp := &model.ExportFormat{Version: 1}
	for _, sub := range subs {
		insts, err := s.ListInstances(sub.Slug)
		if err != nil {
			return nil, err
		}
		es := model.ExportSubscription{Slug: sub.Slug, Name: sub.Name}
		for _, inst := range insts {
			es.Instances = append(es.Instances, model.ExportInstance{
				SourceInstanceID: inst.SourceInstanceID,
				RawURI:           inst.RawURI,
			})
		}
		exp.Subscriptions = append(exp.Subscriptions, es)
	}
	return exp, nil
}

// Import loads subscriptions from the export format. Existing slugs are
// skipped unless overwrite is true.
func (s *Store) Import(data *model.ExportFormat, overwrite bool) (created, skipped int, err error) {
	for _, es := range data.Subscriptions {
		existing, lookupErr := s.GetSubscriptionBySlug(es.Slug)
		if lookupErr == nil && existing != nil {
			if !overwrite {
				skipped++
				continue
			}
			// Overwrite: delete old and re-create.
			if err := s.DeleteSubscription(es.Slug); err != nil {
				return created, skipped, fmt.Errorf("delete %s for overwrite: %w", es.Slug, err)
			}
		}

		sub, cErr := s.CreateSubscription(es.Slug, es.Name)
		if cErr != nil {
			return created, skipped, fmt.Errorf("create %s: %w", es.Slug, cErr)
		}

		for _, ei := range es.Instances {
			now := time.Now().UTC()
			label := extractLabel(ei.RawURI)
			var source any
			if ei.SourceInstanceID != nil && *ei.SourceInstanceID >= 0 {
				source = *ei.SourceInstanceID
			}
			_, iErr := s.db.Exec(
				"INSERT INTO instances (subscription_id, source_instance_id, raw_uri, label, created_at) VALUES (?, ?, ?, ?, ?)",
				sub.ID, source, ei.RawURI, label, now,
			)
			if iErr != nil {
				return created, skipped, fmt.Errorf("add instance to %s: %w", es.Slug, iErr)
			}
		}
		created++
	}
	return created, skipped, nil
}

// extractLabel parses the fragment (#label) from an olcrtc:// URI.
func extractLabel(uri string) string {
	idx := strings.LastIndex(uri, "#")
	if idx < 0 {
		return ""
	}
	return uri[idx+1:]
}

// ensureCoreParam appends core=legacy to an olcrtc:// URI when no core=
// parameter is present, so manual URIs the operator pastes into a subscription
// pick the manager wire format (32-byte VP8) without relying on the client
// default. Linked URIs already carry core=legacy from the admin builders, and
// existing core= values are preserved verbatim.
//
// ponytail: scissors-only string math, no net/url — url.URL.String()
// percent-encodes the fragment a second time and re-sorts query pairs,
// corrupting an already-encoded olcrtc:// URI. The scheme check goes through
// the cheap prefix guard in AddInstanceWithSource, so here we only inspect the
// query (between '?' and the first '#' after it).
func ensureCoreParam(rawURI string) string {
	quest := strings.IndexByte(rawURI, '?')
	hash := strings.IndexByte(rawURI, '#')
	if quest < 0 {
		return appendCoreParam(rawURI, hash)
	}
	queryEnd := len(rawURI)
	if hash > quest {
		queryEnd = hash
	}
	query := rawURI[quest+1 : queryEnd]
	for _, pair := range strings.Split(query, "&") {
		key, _, _ := strings.Cut(pair, "=")
		if key == "core" {
			return rawURI
		}
	}
	return appendCoreParam(rawURI, hash)
}

func appendCoreParam(rawURI string, hash int) string {
	param := "&core=legacy"
	if !strings.Contains(rawURI, "?") {
		param = "?core=legacy"
	}
	if hash < 0 {
		return rawURI + param
	}
	return rawURI[:hash] + param + rawURI[hash:]
}

// backfillCoreParam migrates instances persisted before the manager pinned
// core=legacy into every URI: it rewrites stored raw_uri values that lack a
// core= parameter so the public /sub/{slug} feed, QR subscription and export
// all carry core=legacy without re-adding the instance. Idempotent.
//
// ponytail: one pass over raw_uri; ensureCoreParam is a no-op once core=
// is present, so re-running on already-migrated stores touches no rows.
func backfillCoreParam(db *sql.DB) error {
	rows, err := db.Query("SELECT id, raw_uri FROM instances")
	if err != nil {
		return fmt.Errorf("select instances for core backfill: %w", err)
	}
	var stale []struct {
		id     int64
		rawURI string
	}
	for rows.Next() {
		var id int64
		var rawURI string
		if err := rows.Scan(&id, &rawURI); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan instance for core backfill: %w", err)
		}
		if fixed := ensureCoreParam(rawURI); fixed != rawURI {
			stale = append(stale, struct {
				id     int64
				rawURI string
			}{id, fixed})
		}
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close core backfill cursor: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read core backfill cursor: %w", err)
	}
	for _, s := range stale {
		if _, err := db.Exec("UPDATE instances SET raw_uri = ? WHERE id = ?", s.rawURI, s.id); err != nil {
			return fmt.Errorf("rewrite instance %d core param: %w", s.id, err)
		}
	}
	return nil
}
