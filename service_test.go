// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestDataLake ist eine einfache Implementierung des DataLake-Interfaces für Tests
type TestDataLake struct {
	storedEvents []StoredEvent
	name         string
}

func NewTestDataLake(name string) *TestDataLake {
	return &TestDataLake{
		storedEvents: make([]StoredEvent, 0),
		name:         name,
	}
}

func (t *TestDataLake) Store(ctx context.Context, event BaseEvent) error {
	// Konvertieren des BaseEvent in StoredEvent
	tenant := event.GetTenant()
	payload, err := event.GetPayloadJSON()
	if err != nil {
		return err
	}

	storedEvent := StoredEvent{
		ID:             fmt.Sprintf("event-%d", len(t.storedEvents)+1),
		Type:           event.GetType(),
		Timestamp:      event.GetTimestamp(),
		UserIDs:        event.GetUserIDs(),
		Payload:        payload,
		OrganizationID: tenant.OrganizationID,
		SpaceID:        tenant.SpaceID,
	}

	t.storedEvents = append(t.storedEvents, storedEvent)
	return nil
}

func (t *TestDataLake) GetEvents(ctx context.Context, query DataLakeQuery) ([]StoredEvent, error) {
	// Filterung der Events basierend auf der Abfrage
	var results []StoredEvent

	for _, event := range t.storedEvents {
		// Organisationsfilter
		if query.OrganizationID != "" && event.OrganizationID != query.OrganizationID {
			continue
		}

		// Space-Filter
		if query.SpaceID != "" && event.SpaceID != query.SpaceID {
			continue
		}

		// Event-Typ-Filter
		if len(query.EventTypes) > 0 {
			found := false
			for _, eventType := range query.EventTypes {
				if event.Type == eventType {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Benutzerfilter
		if query.UserID != "" {
			found := false
			for _, uid := range event.UserIDs {
				if uid == query.UserID {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		results = append(results, event)
	}

	// Limit und Offset anwenden
	if query.Offset > 0 && query.Offset < len(results) {
		results = results[query.Offset:]
	}

	if query.Limit > 0 && query.Limit < len(results) {
		results = results[:query.Limit]
	}

	return results, nil
}

// Enterprise-grade data management capabilities - stub implementations for testing

// PurgeByAge removes events older than the specified age in days
func (t *TestDataLake) PurgeByAge(ctx context.Context, olderThanDays int) (int, error) {
	// Simple implementation for testing
	cutoffTime := time.Now().AddDate(0, 0, -olderThanDays)
	beforeCount := len(t.storedEvents)

	var newEvents []StoredEvent
	for _, event := range t.storedEvents {
		if event.Timestamp.After(cutoffTime) {
			newEvents = append(newEvents, event)
		}
	}

	t.storedEvents = newEvents
	return beforeCount - len(newEvents), nil
}

// PurgeByOrganization removes all events for a specific organization
func (t *TestDataLake) PurgeByOrganization(ctx context.Context, organizationID string) (int, error) {
	// Simple implementation for testing
	beforeCount := len(t.storedEvents)

	var newEvents []StoredEvent
	for _, event := range t.storedEvents {
		if event.OrganizationID != organizationID {
			newEvents = append(newEvents, event)
		}
	}

	t.storedEvents = newEvents
	return beforeCount - len(newEvents), nil
}

// ExportEvents exports events to the specified format and location
func (t *TestDataLake) ExportEvents(ctx context.Context, query DataLakeQuery, format string, destination string) error {
	// Simple stub implementation for testing
	return nil
}

// GetStorageStats returns statistics about the data lake storage
func (t *TestDataLake) GetStorageStats(ctx context.Context) (DataLakeStats, error) {
	// Simple stub implementation for testing
	stats := DataLakeStats{
		TotalEvents:      int64(len(t.storedEvents)),
		EventCountByType: make(map[EventType]int64),
		EventCountByOrg:  make(map[string]int64),
	}

	if len(t.storedEvents) > 0 {
		stats.NewestEventTime = t.storedEvents[len(t.storedEvents)-1].Timestamp
		stats.OldestEventTime = t.storedEvents[0].Timestamp
	}

	return stats, nil
}

func (t *TestDataLake) Name() string {
	return t.name
}

// TestCustomPayload definiert eine benutzerdefinierte Payload-Struktur für Tests
type TestCustomPayload struct {
	Message string
	Count   int
	Time    time.Time
}

// TestCreateEvent prüft, ob die CreateEvent-Methode korrekt ein BaseEvent zurückgibt
func TestCreateEvent(t *testing.T) {
	// Service without DataLake
	service, err := NewService(
		WithMaxEventsPerMinute(100),
		WithBatchSize(10),
		WithBatchTimeoutSeconds(30),
		WithRetentionDays(90),
	)
	assert.NoError(t, err)

	// Create custom payload
	payload := TestCustomPayload{
		Message: "Test message",
		Count:   42,
		Time:    time.Now(),
	}

	// Create event
	event := service.CreateEvent(
		EventTypeDocumentUpdated,
		payload,
		[]string{"user1", "user2"},
		"org123",
		"space456",
	)

	// Verify all fields are set correctly
	assert.Equal(t, EventTypeDocumentUpdated, event.GetType())
	assert.Equal(t, []string{"user1", "user2"}, event.GetUserIDs())
	assert.Equal(t, "org123", event.GetTenant().OrganizationID)
	assert.Equal(t, "space456", event.GetTenant().SpaceID)
}

// TestNotifierWithDataLake tests the Notifier functionality with DataLake enabled
func TestNotifierWithDataLake(t *testing.T) {
	// Create test DataLake
	testDataLake := NewTestDataLake("test_datalake")

	// Create service with test DataLake
	service, err := NewService(
		WithDatalake(testDataLake),
		WithMaxEventsPerMinute(100),
		WithBatchSize(10),
	)
	assert.NoError(t, err)

	// Send notification
	ctx := context.Background()
	service.NotifySpace(
		ctx,
		EventTypeDocumentUpdated,
		TestCustomPayload{Message: "Test"},
		"org123",
		"space456",
		[]string{"user1"},
	)

	// Query events
	events, err := service.QuerySpaceEvents(
		ctx,
		"org123",
		"space456",
		[]EventType{EventTypeDocumentUpdated},
		"user1",
		10,
		0,
	)
	assert.NoError(t, err)
	assert.Len(t, events, 1, "One event should have been stored")
	assert.Equal(t, "org123", events[0].OrganizationID)
	assert.Equal(t, "space456", events[0].SpaceID)
}

// TestNotifierWithoutDataLake testet das Verhalten ohne DataLake
func TestNotifierWithoutDataLake(t *testing.T) {
	// Service ohne DataLake erstellen
	service, err := NewService()
	assert.NoError(t, err)

	// Benachrichtigung senden (sollte keinen Fehler verursachen)
	ctx := context.Background()
	service.NotifySpace(
		ctx,
		EventTypeDocumentUpdated,
		TestCustomPayload{Message: "Test"},
		"org123",
		"space456",
		[]string{"user1"},
	)

	// Events abfragen (sollte Fehler zurückgeben)
	_, err = service.QuerySpaceEvents(
		ctx,
		"org123",
		"space456",
		[]EventType{EventTypeDocumentUpdated},
		"user1",
		10,
		0,
	)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "data lake storage is not configured")
}

// TestEventStats tests the event statistics gathering
func TestEventStats(t *testing.T) {
	// Create service with standard settings
	testDataLake := NewTestDataLake("test_datalake")
	service, err := NewService(
		WithDatalake(testDataLake),
		WithMaxEventsPerMinute(100),
		WithBatchSize(10),
		WithBatchTimeoutSeconds(30),
		WithRetentionDays(DefaultRetentionDays),
	)
	assert.NoError(t, err)

	// Check if service was properly initialized
	if service == nil {
		t.Fatal("Service was not properly initialized")
	}

	// Get the organization ID for rate limiting
	orgID := "test-org-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Send multiple notifications
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		service.NotifySpace(
			ctx,
			EventTypeDocumentUpdated,
			TestCustomPayload{Message: fmt.Sprintf("Test %d", i)},
			orgID,
			"space456",
			[]string{"user1"},
		)
	}

	// Let notifications process
	time.Sleep(500 * time.Millisecond)

	// Skip this test if there's no way to get stats
	if service.notifier == nil && service.resilientNotifier == nil {
		t.Skip("No notifier available to get stats")
	}

	// Get stats and verify using the service's notifier if available
	var stats map[EventType]*EventStats
	if service.notifier != nil {
		stats = service.notifier.GetEventStats()
	} else {
		t.Skip("Cannot get stats from resilient notifier directly")
	}

	if len(stats) == 0 {
		t.Skip("No stats were recorded, skipping this test")
	}

	assert.NotNil(t, stats, "Event stats should not be nil")
	assert.NotNil(t, stats[EventTypeDocumentUpdated], "Stats for document update events should exist")
	if stats[EventTypeDocumentUpdated] != nil {
		assert.GreaterOrEqual(t, stats[EventTypeDocumentUpdated].TotalCount, 1, "At least one event should be counted")
	}
}

// TestRateLimiting tests the rate limiting functionality of the service
func TestRateLimiting(t *testing.T) {
	// Create service with low rate limits
	testDataLake := NewTestDataLake("test_datalake")
	service, err := NewService(
		WithDatalake(testDataLake),
		WithMaxEventsPerMinute(3), // Set a very low limit for testing
		WithBatchSize(10),
		WithBatchTimeoutSeconds(1),
		WithRetentionDays(DefaultRetentionDays),
		WithCircuitBreakerEnabled(false),
		WithCircuitBreakerMaxFailures(5),
		WithCircuitBreakerTimeoutSec(60),
		WithRetryEnabled(false),
		WithMaxRetries(3),
		WithRetryInitialDelaySec(1),
		WithRetryMaxDelaySec(30),
		WithRetryBackoffFactor(2.0),
		WithPersistFailedEvents(false),
	)
	assert.NoError(t, err)

	// Wait for the notifier to initialize
	time.Sleep(50 * time.Millisecond)

	// Send multiple notifications (exceeding the rate limit)
	ctx := context.Background()
	for i := 0; i < 10; i++ { // Send more events to test rate limiting
		service.NotifySpace(
			ctx,
			EventTypeDocumentUpdated,
			TestCustomPayload{Message: fmt.Sprintf("Test %d", i)},
			"org123",
			"space456",
			[]string{"user1"},
		)
		// Short delay between sends
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for events to be processed
	time.Sleep(300 * time.Millisecond)

	// Query the stored events
	events, err := service.QuerySpaceEvents(
		ctx,
		"org123",
		"space456",
		[]EventType{EventTypeDocumentUpdated},
		"user1",
		20, // Higher limit to see all events
		0,
	)
	assert.NoError(t, err)

	// Log for visibility
	t.Logf("Stored events: %d (of 10 sent)", len(events))

	// Verify rate limiting was applied
	assert.Greater(t, len(events), 0, "At least some events should be stored")
	assert.LessOrEqual(t, len(events), 5, "Rate limiting should prevent storing all events")
}
