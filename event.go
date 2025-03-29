// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"encoding/json"
	"time"
)

// TenantInfo holds the tenant context information for multi-tenancy support
type TenantInfo struct {
	OrganizationID string `json:"organizationId,omitempty"`
	SpaceID        string `json:"spaceId,omitempty"`
}

// Event is the base structure for all events that are
// processed by the notifier, with generic payload type
type Event[T any] struct {
	Type      EventType  `json:"type"`
	Timestamp time.Time  `json:"timestamp"`
	Payload   T          `json:"payload"`
	UserIDs   []string   `json:"userIds,omitempty"` // IDs of users to be notified
	Tenant    TenantInfo `json:"tenant"`            // Tenant context for isolation
}

// BaseEvent provides a non-generic interface to work with events
// regardless of their payload type
type BaseEvent interface {
	GetType() EventType
	GetTimestamp() time.Time
	GetUserIDs() []string
	GetPayloadJSON() ([]byte, error)
	GetTenant() TenantInfo
}

// GetType returns the event type
func (e *Event[T]) GetType() EventType {
	return e.Type
}

// GetTimestamp returns the event timestamp
func (e *Event[T]) GetTimestamp() time.Time {
	return e.Timestamp
}

// GetUserIDs returns the list of user IDs to be notified
func (e *Event[T]) GetUserIDs() []string {
	return e.UserIDs
}

// GetPayloadJSON returns the event payload as JSON bytes
func (e *Event[T]) GetPayloadJSON() ([]byte, error) {
	return json.Marshal(e.Payload)
}

// GetTenant returns the tenant information
func (e *Event[T]) GetTenant() TenantInfo {
	return e.Tenant
}
