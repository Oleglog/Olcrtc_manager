// Package store provides SQLite-backed CRUD operations for subscriptions and instances.
package store

import (
	"database/sql"
	"errors"
	"fmt"
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

	// WAL mode for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
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
    raw_uri         TEXT    NOT NULL,
    label           TEXT,
    created_at      DATETIME NOT NULL
);
`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate schema: %w", err)
	}
	// Enable FK enforcement.
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	return err
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
	if !strings.HasPrefix(rawURI, "olcrtc://") {
		return nil, ErrInvalidURI
	}

	sub, err := s.GetSubscriptionBySlug(slug)
	if err != nil {
		return nil, err
	}

	label := extractLabel(rawURI)
	now := time.Now().UTC()

	res, err := s.db.Exec(
		"INSERT INTO instances (subscription_id, raw_uri, label, created_at) VALUES (?, ?, ?, ?)",
		sub.ID, rawURI, label, now,
	)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return &model.Instance{
		ID: id, SubscriptionID: sub.ID,
		RawURI: rawURI, Label: label, CreatedAt: now,
	}, nil
}

// ListInstances returns all instances for a subscription.
func (s *Store) ListInstances(slug string) ([]model.Instance, error) {
	sub, err := s.GetSubscriptionBySlug(slug)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(
		"SELECT id, subscription_id, raw_uri, label, created_at FROM instances WHERE subscription_id = ? ORDER BY created_at",
		sub.ID,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var insts []model.Instance
	for rows.Next() {
		var inst model.Instance
		if err := rows.Scan(&inst.ID, &inst.SubscriptionID, &inst.RawURI, &inst.Label, &inst.CreatedAt); err != nil {
			return nil, err
		}
		insts = append(insts, inst)
	}
	return insts, rows.Err()
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
			es.Instances = append(es.Instances, model.ExportInstance{RawURI: inst.RawURI})
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
			_, iErr := s.db.Exec(
				"INSERT INTO instances (subscription_id, raw_uri, label, created_at) VALUES (?, ?, ?, ?)",
				sub.ID, ei.RawURI, label, now,
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
