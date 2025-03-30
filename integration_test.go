// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kopexa-grc/notifier"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace/noop"
)

// TestEvent implements BaseEvent for our integration tests
type TestEvent struct {
	Type      notifier.EventType
	OrgID     string
	SpaceID   string
	Payload   string
	Timestamp time.Time
	Users     []string
}

func (e *TestEvent) GetType() notifier.EventType {
	return e.Type
}

func (e *TestEvent) GetTenant() notifier.TenantInfo {
	return notifier.TenantInfo{
		OrganizationID: e.OrgID,
		SpaceID:        e.SpaceID,
	}
}

func (e *TestEvent) GetPayload() interface{} {
	return e.Payload
}

func (e *TestEvent) GetPayloadJSON() ([]byte, error) {
	return []byte(e.Payload), nil
}

func (e *TestEvent) GetUserIDs() []string {
	if e.Users == nil {
		return []string{"test-user-1", "test-user-2"}
	}
	return e.Users
}

func (e *TestEvent) GetTimestamp() time.Time {
	if e.Timestamp.IsZero() {
		return time.Now()
	}
	return e.Timestamp
}

// MockProvider implements the Provider interface for testing
type MockProvider struct {
	name      string
	lock      sync.Mutex
	events    []notifier.BaseEvent
	callCount int
}

func NewMockProvider(name string) *MockProvider {
	return &MockProvider{
		name:   name,
		events: make([]notifier.BaseEvent, 0),
	}
}

func (m *MockProvider) Send(ctx context.Context, event notifier.BaseEvent) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.events = append(m.events, event)
	m.callCount++
	return nil
}

func (m *MockProvider) Name() string {
	return m.name
}

func (m *MockProvider) EventCount() int {
	m.lock.Lock()
	defer m.lock.Unlock()
	return len(m.events)
}

// FailingMockProvider simulates a provider that fails occasionally
type FailingMockProvider struct {
	name           string
	lock           sync.Mutex
	events         []notifier.BaseEvent
	callCount      int
	failAfter      int
	recoveredAfter int
	currentCalls   int
	failCount      int
}

func NewFailingMockProvider(name string, failAfter, recoveredAfter int) *FailingMockProvider {
	return &FailingMockProvider{
		name:           name,
		events:         make([]notifier.BaseEvent, 0),
		failAfter:      failAfter,
		recoveredAfter: recoveredAfter,
	}
}

func (m *FailingMockProvider) Send(ctx context.Context, event notifier.BaseEvent) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.callCount++
	m.currentCalls++

	// Fail after certain number of successful calls
	if m.currentCalls > m.failAfter && m.failCount < m.recoveredAfter {
		m.failCount++
		return errors.New("simulated failure in provider")
	}

	// Reset fail count after recovery
	if m.failCount >= m.recoveredAfter {
		m.failCount = 0
		m.currentCalls = 0
	}

	m.events = append(m.events, event)
	return nil
}

func (m *FailingMockProvider) Name() string {
	return m.name
}

func (m *FailingMockProvider) EventCount() int {
	m.lock.Lock()
	defer m.lock.Unlock()
	return len(m.events)
}

// MockDataLake implements the DataLake interface for testing
type MockDataLake struct {
	name         string
	lock         sync.Mutex
	storedEvents []notifier.StoredEvent
	callCount    int
}

func NewMockDataLake(name string) *MockDataLake {
	return &MockDataLake{
		name:         name,
		storedEvents: make([]notifier.StoredEvent, 0),
	}
}

func (m *MockDataLake) Store(ctx context.Context, event notifier.BaseEvent) error {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.callCount++
	tenant := event.GetTenant()

	m.storedEvents = append(m.storedEvents, notifier.StoredEvent{
		ID:             fmt.Sprintf("mock-event-%d", len(m.storedEvents)),
		Type:           event.GetType(),
		Timestamp:      time.Now(),
		UserIDs:        event.GetUserIDs(),
		OrganizationID: tenant.OrganizationID,
		SpaceID:        tenant.SpaceID,
	})

	return nil
}

func (m *MockDataLake) GetEvents(ctx context.Context, query notifier.DataLakeQuery) ([]notifier.StoredEvent, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	// Return all events - in a real implementation we would filter by query
	result := make([]notifier.StoredEvent, len(m.storedEvents))
	copy(result, m.storedEvents)
	return result, nil
}

func (m *MockDataLake) PurgeOldEvents(ctx context.Context, olderThan time.Time) (int, error) {
	// Not needed for these tests
	return 0, nil
}

// PurgeByAge removes events older than the specified age in days
func (m *MockDataLake) PurgeByAge(ctx context.Context, olderThanDays int) (int, error) {
	// Not needed for these tests
	return 0, nil
}

// PurgeByOrganization removes all events for a specific organization
func (m *MockDataLake) PurgeByOrganization(ctx context.Context, organizationID string) (int, error) {
	// Not needed for these tests
	return 0, nil
}

func (m *MockDataLake) ExportEvents(ctx context.Context, query notifier.DataLakeQuery, format string, destination string) error {
	// Not needed for these tests
	return nil
}

func (m *MockDataLake) GetStatistics(ctx context.Context, query notifier.DataLakeQuery) (notifier.DataLakeStats, error) {
	// Not needed for these tests
	return notifier.DataLakeStats{}, nil
}

// GetStorageStats returns statistics about the data lake storage
func (m *MockDataLake) GetStorageStats(ctx context.Context) (notifier.DataLakeStats, error) {
	// Not needed for these tests
	return notifier.DataLakeStats{
		TotalEvents: int64(len(m.storedEvents)),
	}, nil
}

// Name returns the name of the DataLake
func (m *MockDataLake) Name() string {
	return m.name
}

// TestIntegration_NotifierWithPool tests the integration of the provider pool with the ResilientNotifier
func TestIntegration_NotifierWithPool(t *testing.T) {
	// Test configuration
	notifierConfig := notifier.NotifierConfig{
		MaxEventsPerMinute:  100, // Ensure valid value
		BatchSize:           10,
		BatchTimeoutSeconds: 1,
		RetentionDays:       30,
	}

	resilienceConfig := notifier.ResilienceConfig{
		CircuitBreakerEnabled:     true,
		CircuitBreakerMaxFailures: 3,
		CircuitBreakerTimeout:     1 * time.Second, // Changed from 200ms to meet the 1s minimum requirement
		RetryEnabled:              true,
		MaxRetries:                2,
		RetryInitialDelay:         50 * time.Millisecond,
		RetryMaxDelay:             1 * time.Second, // Changed to ensure it's sufficient
		RetryBackoffFactor:        2.0,
	}

	// Create provider pool
	pool := notifier.NewProviderPool(5, 1*time.Minute)

	// Register mock provider factories: one reliable and one unreliable
	reliableProviderCreated := false
	unreliableProviderCreated := false

	// Reliable provider
	pool.RegisterProviderFactory("reliable", func() (notifier.Provider, error) {
		reliableProviderCreated = true
		return NewMockProvider("reliable"), nil
	})

	// Unreliable provider that fails occasionally
	pool.RegisterProviderFactory("unreliable", func() (notifier.Provider, error) {
		unreliableProviderCreated = true
		return NewFailingMockProvider("unreliable", 1, 2), nil
	})

	// Create DataLake mock
	dataLake := NewMockDataLake("test-datalake")

	// Create ResilientNotifier with pool
	rNotifier, err := notifier.NewResilientNotifierWithPool(
		notifierConfig,
		resilienceConfig,
		noop.NewTracerProvider().Tracer(""),
		nil,
		pool,
		"reliable", "unreliable", // Provider types
	)
	require.NoError(t, err, "Creating notifier should succeed")
	require.NotNil(t, rNotifier, "Notifier should not be nil")

	// Verify provider factories were called
	assert.True(t, reliableProviderCreated, "Reliable provider should have been created")
	assert.True(t, unreliableProviderCreated, "Unreliable provider should have been created")

	// Set DataLake
	rNotifier.SetDataLake(dataLake)

	// Start notifier
	rNotifier.Start()
	defer rNotifier.Stop()

	// Create test events using the exported types
	event1 := createTestEvent(notifier.EventTypeInfo, "org1", "space1", "test-event-1")
	event2 := createTestEvent(notifier.EventTypeWarning, "org1", "space1", "test-event-2")
	event3 := createTestEvent(notifier.EventTypeCritical, "org2", "space2", "test-event-3")

	// Test 1: Send event to both providers (should succeed)
	rNotifier.Notify(context.Background(), event1)

	// Wait briefly to allow async operations to complete
	time.Sleep(100 * time.Millisecond)

	// Test 2: Send more events (the unreliable provider will fail)
	rNotifier.Notify(context.Background(), event2)
	rNotifier.Notify(context.Background(), event3)

	// Wait until the Circuit Breaker transitions to HALF_OPEN state
	time.Sleep(300 * time.Millisecond)

	// Check Circuit Breaker status
	cbStatus := rNotifier.GetCircuitBreakerStatus()
	t.Logf("Circuit Breaker Status: %v", cbStatus)

	// Send more events to test the retry mechanism
	event4 := createTestEvent(notifier.EventTypeUpdate, "org2", "space2", "test-event-4")
	rNotifier.Notify(context.Background(), event4)

	// Wait for the retry mechanism to attempt to resend events
	time.Sleep(300 * time.Millisecond)

	// Check retry status
	retryStatus := rNotifier.GetRetryStatus()
	t.Logf("Retry Status: %v", retryStatus)

	// Check DataLake entries
	storedEvents, err := dataLake.GetEvents(context.Background(), notifier.DataLakeQuery{})
	require.NoError(t, err, "DataLake query should succeed")
	t.Logf("Number of events stored in DataLake: %d", len(storedEvents))

	// There should be at least the original events in the DataLake
	assert.GreaterOrEqual(t, len(storedEvents), 4, "There should be at least 4 events in the DataLake")
}

// TestIntegration_ServiceWithPool tests the integration of the provider pool with the Service
func TestIntegration_ServiceWithPool(t *testing.T) {
	// Create pool for the service
	pool := notifier.NewProviderPool(5, 1*time.Minute)

	// Register mock provider
	mockProviderCreated := false
	pool.RegisterProviderFactory("mock", func() (notifier.Provider, error) {
		mockProviderCreated = true
		return NewMockProvider("mock"), nil
	})

	// Verify that the provider has not been created yet
	assert.False(t, mockProviderCreated, "Provider should not have been created yet")

	// Create DataLake for service
	dataLake := NewMockDataLake("service-datalake")

	// Create service with options
	service, err := notifier.NewService(
		notifier.WithDatalake(dataLake),
		notifier.WithBatchSize(10),
		notifier.WithBatchTimeoutSeconds(1),
		notifier.WithRetentionDays(30),
		notifier.WithCircuitBreakerEnabled(true),
		notifier.WithCircuitBreakerMaxFailures(3),
		notifier.WithCircuitBreakerTimeoutSec(1),
		notifier.WithRetryEnabled(true),
		notifier.WithMaxRetries(2),
		notifier.WithRetryInitialDelaySec(1),
		notifier.WithRetryMaxDelaySec(5),
		notifier.WithRetryBackoffFactor(2.0),
		notifier.WithPersistFailedEvents(true),
	)
	require.NoError(t, err, "Service creation should succeed")
	require.NotNil(t, service, "Service should not be nil")

	// Send events to the service
	ctx := context.Background()
	service.NotifyOrganization(ctx, notifier.EventTypeInfo, "Test Payload", "org1", []string{"user1", "user2"})
	service.NotifySpace(ctx, notifier.EventTypeWarning, "Test Space Payload", "org1", "space1", []string{"user1"})

	// Wait briefly to allow async operations to complete
	time.Sleep(100 * time.Millisecond)

	// Check DataLake entries
	storedEvents, err := dataLake.GetEvents(ctx, notifier.DataLakeQuery{})
	require.NoError(t, err, "DataLake query should succeed")

	// At least 2 events should be in the DataLake
	assert.GreaterOrEqual(t, len(storedEvents), 2, "There should be at least 2 events in the DataLake")

	// Check health metrics
	health := service.GetNotifierHealth()
	t.Logf("Notifier Health: %v", health)

	// Reset Circuit Breaker
	service.ResetAllCircuitBreakers()
}

// TestIntegration_ErrorCases tests various error cases and their handling
func TestIntegration_ErrorCases(t *testing.T) {
	// Test 1: Invalid configuration
	invalidConfig := notifier.NotifierConfig{
		MaxEventsPerMinute: -1, // Invalid: too low
	}

	resilienceConfig := notifier.ResilienceConfig{
		CircuitBreakerEnabled: true,
		RetryEnabled:          true,
	}

	_, err := notifier.NewResilientNotifier(invalidConfig, resilienceConfig, nil, nil)
	assert.Error(t, err, "Invalid configuration should cause an error")
	assert.Contains(t, err.Error(), "MaxEventsPerMinute", "Error message should name the problem")

	// Test 2: Provider factory error
	pool := notifier.NewProviderPool(3, time.Minute)
	pool.RegisterProviderFactory("failing-factory", func() (notifier.Provider, error) {
		return nil, errors.New("provider creation failed")
	})

	// Try to retrieve provider
	_, err = pool.GetProvider(context.Background(), "failing-factory")
	assert.Error(t, err, "Factory error should be propagated")
	assert.Contains(t, err.Error(), "provider creation failed")

	// Test 3: Unregistered provider type
	_, err = pool.GetProvider(context.Background(), "non-existent")
	assert.Error(t, err, "Unregistered type should cause error")

	// Test 4: Invalid provider type in NotifierWithPool
	validConfig := notifier.NotifierConfig{
		MaxEventsPerMinute:  100,
		BatchSize:           10,
		BatchTimeoutSeconds: 1,
		RetentionDays:       30,
	}

	// Create a valid resilience config for this test
	validResilienceConfig := notifier.ResilienceConfig{
		CircuitBreakerEnabled:     true,
		CircuitBreakerMaxFailures: 3,
		CircuitBreakerTimeout:     1 * time.Second,
		RetryEnabled:              true,
		MaxRetries:                2,
		RetryInitialDelay:         50 * time.Millisecond,
		RetryMaxDelay:             1 * time.Second,
		RetryBackoffFactor:        2.0,
	}

	_, err = notifier.NewResilientNotifierWithPool(
		validConfig,
		validResilienceConfig,
		nil, nil,
		pool,
		"non-existent", // Unregistered provider type
	)
	assert.Error(t, err, "Unregistered provider type should cause error")
	// The actual error contains provider type information
	assert.Contains(t, err.Error(), "non-existent", "Error message should include the provider type")
}

// Helper function to create test events
func createTestEvent(eventType notifier.EventType, orgID, spaceID, payload string) notifier.BaseEvent {
	return &TestEvent{
		Type:      eventType,
		OrgID:     orgID,
		SpaceID:   spaceID,
		Payload:   payload,
		Timestamp: time.Now(),
	}
}
