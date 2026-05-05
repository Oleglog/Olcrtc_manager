// Package model defines data structures for the subscription system.
package model

import "time"

// Subscription represents a named subscription with a permanent slug URL.
type Subscription struct {
	ID        int64     `json:"id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Instance represents a single olcrtc:// connection URI bound to a subscription.
type Instance struct {
	ID             int64     `json:"id"`
	SubscriptionID int64     `json:"subscription_id"`
	RawURI         string    `json:"raw_uri"`
	Label          string    `json:"label,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ExportFormat is the top-level JSON structure for import/export.
type ExportFormat struct {
	Version       int                  `json:"version"`
	Subscriptions []ExportSubscription `json:"subscriptions"`
}

// ExportSubscription is a single subscription entry in the export file.
type ExportSubscription struct {
	Slug      string           `json:"slug"`
	Name      string           `json:"name"`
	Instances []ExportInstance `json:"instances"`
}

// ExportInstance is a single instance entry in the export file.
type ExportInstance struct {
	RawURI string `json:"raw_uri"`
}
