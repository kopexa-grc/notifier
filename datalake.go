// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"encoding/json"
	"time"
)

// DataLake defines the interface for persisting notification events
type DataLake interface {
	// Store persists an event to the data lake
	Store(ctx context.Context, event BaseEvent) error

	// GetEvents retrieves events based on query parameters
	GetEvents(ctx context.Context, query DataLakeQuery) ([]StoredEvent, error)

	// PurgeByAge removes events older than the specified age in days
	PurgeByAge(ctx context.Context, olderThanDays int) (int, error)

	// PurgeByOrganization removes all events for a specific organization
	PurgeByOrganization(ctx context.Context, organizationID string) (int, error)

	// ExportEvents exports events to the specified format and location
	ExportEvents(ctx context.Context, query DataLakeQuery, format string, destination string) error

	// GetStorageStats returns statistics about the data lake storage
	GetStorageStats(ctx context.Context) (DataLakeStats, error)

	// Name returns the name of the storage provider
	Name() string
}

// DataLakeQuery defines the parameters for querying events from the data lake
type DataLakeQuery struct {
	EventTypes     []EventType `json:"eventTypes,omitempty"`
	UserID         string      `json:"userId,omitempty"`
	OrganizationID string      `json:"organizationId,omitempty"` // Filter by organization
	SpaceID        string      `json:"spaceId,omitempty"`        // Filter by space
	StartTime      time.Time   `json:"startTime,omitempty"`
	EndTime        time.Time   `json:"endTime,omitempty"`
	Limit          int         `json:"limit,omitempty"`
	Offset         int         `json:"offset,omitempty"`
}

// StoredEvent represents an event as retrieved from the data lake
type StoredEvent struct {
	ID             string          `json:"id"`
	Type           EventType       `json:"type"`
	Timestamp      time.Time       `json:"timestamp"`
	UserIDs        []string        `json:"userIds"`
	Payload        json.RawMessage `json:"payload"`
	OrganizationID string          `json:"organizationId,omitempty"`
	SpaceID        string          `json:"spaceId,omitempty"`
}

// DataLakeStats provides metrics about the data lake storage
type DataLakeStats struct {
	TotalEvents       int64
	OldestEventTime   time.Time
	NewestEventTime   time.Time
	StorageUsageBytes int64
	EventCountByType  map[EventType]int64
	EventCountByOrg   map[string]int64
	AvgEventsPerDay   float64
}
