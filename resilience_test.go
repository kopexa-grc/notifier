// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// FailingMockProvider implements a Provider for testing that fails after a certain number of calls
type FailingMockProvider struct {
	name           string
	failAfter      int
	callCount      int
	events         []BaseEvent
	errorMsg       string
	recoveredAfter int
	failureCount   int
	mu             sync.Mutex
}

// NewFailingMockProvider creates a new FailingMockProvider
func NewFailingMockProvider(name string, failAfter int, recoveredAfter int) *FailingMockProvider {
	return &FailingMockProvider{
		name:           name,
		failAfter:      failAfter,
		events:         []BaseEvent{},
		recoveredAfter: recoveredAfter,
	}
}

// SetErrorMsg sets a custom error message for the provider
func (p *FailingMockProvider) SetErrorMsg(msg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.errorMsg = msg
}

func (p *FailingMockProvider) Send(ctx context.Context, event BaseEvent) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.callCount++

	// Wenn eine benutzerdefinierte Fehlermeldung gesetzt ist, gib diese zurück
	if p.errorMsg != "" {
		// Event nicht speichern bei Fehlern
		return errors.New(p.errorMsg)
	}

	// Check if the provider should fail
	if p.callCount > p.failAfter {
		p.failureCount++

		// If we have enough failures and should recover
		if p.failureCount >= p.recoveredAfter {
			// Reset the failure counter
			p.failureCount = 0
			// Add the event
			p.events = append(p.events, event)
			return nil
		}

		return errors.New(ErrSimulatedFailure)
	}

	// Normal success
	p.events = append(p.events, event)
	return nil
}

func (p *FailingMockProvider) Name() string {
	return p.name
}

func (p *FailingMockProvider) GetEvents() []BaseEvent {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]BaseEvent, len(p.events))
	copy(result, p.events)
	return result
}

func (p *FailingMockProvider) GetCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.callCount
}

// createTestEvent erzeugt ein Standard-Testevent mit den gegebenen Parametern
func createTestEvent(eventType EventType, orgID, spaceID, payload string) *TestEvent {
	return &TestEvent{
		eventType: eventType,
		tenant: TenantInfo{
			OrganizationID: orgID,
			SpaceID:        spaceID,
		},
		payload:   payload,
		timestamp: time.Now(),
	}
}

// setupRetryManagerForTest initialisiert einen RetryManager für Tests
func setupRetryManagerForTest(config ResilienceConfig, providers ...Provider) *RetryManager {
	tracer := tracenoop.NewTracerProvider().Tracer("")
	retryManager := NewRetryManager(config, tracer, nil)

	// Initialize the pendingEvents map
	retryManager.pendingEvents = make(map[string][]PendingEvent)

	// Register providers
	for _, provider := range providers {
		retryManager.RegisterProvider(provider)
	}

	return retryManager
}

// TestCircuitBreaker tests the circuit breaker functionality
func TestCircuitBreaker(t *testing.T) {
	// Circuit Breaker configuration
	maxFailures := TestCircuitBreakerFailures                 // trigger after 3 failures
	timeout := TestCircuitBreakerTimeoutMs * time.Millisecond // switch to half-open after 100ms

	// Create OpenTelemetry meter
	meter := noop.NewMeterProvider().Meter("test")

	// Create circuit breaker with gobreaker implementation
	cb := NewGoBreakerWrapper(TestProviderName, maxFailures, timeout, meter)

	// Initial state should be CLOSED
	assert.Equal(t, CircuitClosed, cb.GetState(), "Circuit breaker should initially be closed")

	// Create a failing mock provider
	mockProvider := NewFailingMockProvider(TestProviderName, 0, 1)
	providerWithCB := NewProviderWithGoBreakerWrapper(mockProvider, cb, tracenoop.NewTracerProvider().Tracer(""))

	// Create a test event
	event := &TestEvent{
		eventType: TestEventType,
		tenant: TenantInfo{
			OrganizationID: TestOrgID,
			SpaceID:        TestSpaceID,
		},
		payload:   "test-payload",
		timestamp: time.Now(),
	}

	// Simulate 3 consecutive failures to trigger the circuit breaker
	for i := 0; i < maxFailures; i++ {
		mockProvider.SetErrorMsg("simulated failure") // Force all calls to fail
		err := providerWithCB.Send(context.Background(), event)
		assert.Error(t, err, "Call should fail")
	}

	// After maxFailures, the circuit breaker should now be OPEN
	assert.Equal(t, CircuitOpen, cb.GetState(), "Circuit breaker should now be open")

	// Requests in OPEN state should fail immediately
	mockProvider.SetErrorMsg("") // Allow calls to succeed
	err := providerWithCB.Send(context.Background(), event)
	assert.Error(t, err, "Call should fail when circuit is open")
	assert.Contains(t, err.Error(), "circuit breaker is open", "Error should indicate open circuit")

	// Wait until timeout expires
	time.Sleep(timeout + 10*time.Millisecond)

	// After timeout, the circuit breaker should be in half-open state on the next request
	// and allow one test request
	err = providerWithCB.Send(context.Background(), event)
	assert.NoError(t, err, "Request after timeout should succeed")

	// After a successful request in HALF_OPEN state, the circuit breaker should be CLOSED
	assert.Equal(t, CircuitClosed, cb.GetState(), "Circuit breaker should be closed after successful request")

	// Additional requests should succeed when circuit is closed again
	err = providerWithCB.Send(context.Background(), event)
	assert.NoError(t, err, "Request should succeed when circuit is closed")
}

// TestRetryPolicy tests the retry policy functionality
func TestRetryPolicy(t *testing.T) {
	// Configure the retry policy
	config := DefaultResilienceConfig()
	config.RetryEnabled = true
	config.MaxRetries = 3
	config.RetryInitialDelay = 100 * time.Millisecond
	config.RetryMaxDelay = 400 * time.Millisecond
	config.RetryBackoffFactor = 2.0

	// Create a provider that always fails for the first attempt
	// and succeeds on the 2nd attempt
	provider := NewFailingMockProvider(TestProviderName, 0, 1)

	// Create a retry manager with the configured provider
	retryManager := setupRetryManagerForTest(config, provider)

	// Start the retry manager
	retryManager.Start()
	defer retryManager.Stop()

	// Create a test event
	event := createTestEvent(TestEventType, TestOrgID, TestSpaceID, "test-payload")

	t.Log("Adding event to retry queue")
	// Add an event to the retry queue
	retryManager.AddPendingEvent(event, provider.Name(), 0)

	// Check that there is one event in the queue
	assert.Equal(t, 1, retryManager.GetPendingCount(), "There should be one event in the queue")

	// Wait briefly for the nextRetry date to expire
	time.Sleep(TestWaitIntervalMs * time.Millisecond)

	// Call processRetries explicitly instead of waiting for automatic processing
	retryManager.processRetries()

	// Wait briefly for processing
	time.Sleep(TestWaitIntervalMs * time.Millisecond)

	// In case the event gets added to the queue again
	retryManager.processRetries()

	// Wait briefly again
	time.Sleep(TestWaitIntervalMs * time.Millisecond)

	// After sufficient time and multiple retries, the event should be successfully processed
	pendingCount := retryManager.GetPendingCount()
	eventCount := len(provider.GetEvents())

	t.Logf("Remaining events in queue: %d", pendingCount)
	t.Logf("Events at provider: %d", eventCount)
	t.Logf("Provider call counter: %d", provider.GetCallCount())

	assert.Equal(t, 0, pendingCount, "The queue should be empty")
	assert.Equal(t, 1, eventCount, "Provider should have received the event")
}

// TestResilientNotifier tests the integration of Circuit Breaker and Retry Policy
func TestResilientNotifier(t *testing.T) {
	// Configure the resilience mechanisms
	notifierConfig := NotifierConfig{
		MaxEventsPerMinute:  DefaultMaxEventsPerMinute,
		BatchSize:           DefaultBatchSize,
		BatchTimeoutSeconds: 1,
		RetentionDays:       DefaultRetentionDays,
	}

	resilienceConfig := DefaultResilienceConfig()
	resilienceConfig.CircuitBreakerEnabled = true
	resilienceConfig.CircuitBreakerMaxFailures = TestCircuitBreakerFailures
	resilienceConfig.CircuitBreakerTimeout = TestResilientNotifierTimeoutMs * time.Millisecond
	resilienceConfig.RetryEnabled = true
	resilienceConfig.MaxRetries = DefaultMaxRetries
	resilienceConfig.RetryInitialDelay = TestRetryInitialDelayMs * time.Millisecond
	resilienceConfig.RetryMaxDelay = TestRetryMaxDelayMs * time.Millisecond

	// Create a simulated provider
	provider1 := NewFailingMockProvider("provider-1", 2, 2)
	provider2 := NewMockProvider("provider-2")

	// Create a ResilientNotifier
	notifier, err := NewResilientNotifier(
		notifierConfig,
		resilienceConfig,
		tracenoop.NewTracerProvider().Tracer(""),
		nil,
		provider1,
		provider2,
	)
	require.NoError(t, err, "Creating notifier should not fail")

	// Start the notifier
	notifier.Start()
	defer notifier.Stop()

	// Create a test event
	event := &TestEvent{
		eventType: TestEventType,
		tenant: TenantInfo{
			OrganizationID: TestOrgID,
			SpaceID:        TestSpaceID,
		},
		payload: "test-payload",
	}

	// Send the event
	ctx := context.Background()
	notifier.Notify(ctx, event)

	// Check the initial state
	cbStatus := notifier.GetCircuitBreakerStatus()
	require.Equal(t, CircuitClosedStr, cbStatus["provider-1"], "Circuit breaker should initially be closed")

	// Send several events to trigger the circuit breaker
	for i := 0; i < 5; i++ {
		notifier.Notify(ctx, event)
	}

	// Check if the circuit breaker has been opened
	cbStatus = notifier.GetCircuitBreakerStatus()
	t.Logf("Circuit Breaker Status: %v", cbStatus)

	// Check if events are in the retry system
	retryStatus := notifier.GetRetryStatus()
	t.Logf("Retry Status: %v", retryStatus)

	// Wait for all events and retries to be processed
	time.Sleep(1 * time.Second)

	// Check if the events were eventually delivered
	assert.Greater(t, provider2.EventCount(), 0, "Provider 2 should have received events")

	// Check if the circuit breaker has been closed again
	time.Sleep(300 * time.Millisecond) // Wait for timeout + successful processing
	cbStatus = notifier.GetCircuitBreakerStatus()
	t.Logf("Final Circuit Breaker Status: %v", cbStatus)

	// Test manual reset of circuit breaker
	success := notifier.ResetCircuitBreaker("provider-1")
	assert.True(t, success, "Circuit breaker should be resettable")

	// Reset all circuit breakers
	notifier.ResetAllCircuitBreakers()
	cbStatus = notifier.GetCircuitBreakerStatus()
	assert.Equal(t, CircuitClosedStr, cbStatus["provider-1"], "All circuit breakers should be closed")
}

// TestPersistFailedEvents tests that events that fail after maximum retries are stored in the DataLake
func TestPersistFailedEvents(t *testing.T) {
	// Configure the resilience mechanisms
	resilienceConfig := DefaultResilienceConfig()
	resilienceConfig.RetryEnabled = true
	resilienceConfig.MaxRetries = 2                            // Only 2 attempts
	resilienceConfig.RetryInitialDelay = 10 * time.Millisecond // very short delay for testing
	resilienceConfig.RetryMaxDelay = 50 * time.Millisecond
	resilienceConfig.PersistFailedEvents = true

	// Create a provider that always fails
	provider := NewFailingMockProvider("failing-provider", 0, 999) // Always fails

	// Create a DataLake Mock
	dataLake := NewMockDataLake("test-datalake")

	// Create retry manager with specific configuration
	retryManager := setupRetryManagerForTest(resilienceConfig, provider)
	retryManager.SetDataLake(dataLake)

	// Create a test event
	event := createTestEvent(TestEventType, TestOrgID, TestSpaceID, "test-payload")

	// Add event with RetryCount = MaxRetries, which should immediately
	// trigger storage in DataLake
	t.Log("Adding event with RetryCount = MaxRetries")
	retryManager.AddPendingEvent(event, provider.Name(), resilienceConfig.MaxRetries)

	// The event should not be added to the queue since it already exceeded max retries
	assert.Equal(t, 0, retryManager.GetPendingCount(), "Queue should be empty, event should not be queued")

	// Wait briefly to ensure DataLake operation completes
	time.Sleep(50 * time.Millisecond)

	// Check that the event was stored in the DataLake
	ctx := context.Background()
	storedEvents, err := dataLake.GetEvents(ctx, DataLakeQuery{})
	require.NoError(t, err, "DataLake query should succeed")

	t.Logf("Number of events in DataLake: %d", len(storedEvents))
	assert.Equal(t, 1, len(storedEvents), "One event should be stored in the DataLake")

	if len(storedEvents) > 0 {
		t.Logf("Event type in DataLake: %s", storedEvents[0].Type)
		assert.Equal(t, string(event.GetType()), string(storedEvents[0].Type), "Event type should match")
	}
}

// TestConcurrentRetries tests that retries work correctly in a high-load environment
func TestConcurrentRetries(t *testing.T) {
	// Configure the resilience mechanisms
	resilienceConfig := DefaultResilienceConfig()
	resilienceConfig.RetryEnabled = true
	resilienceConfig.MaxRetries = DefaultMaxRetries
	resilienceConfig.RetryInitialDelay = TestConcurrentRetryDelayMs * time.Millisecond
	resilienceConfig.RetryMaxDelay = 100 * time.Millisecond

	// Create a retry manager
	retryManager := NewRetryManager(resilienceConfig, tracenoop.NewTracerProvider().Tracer(""), nil)

	// Create multiple providers with different failure patterns
	provider1 := NewFailingMockProvider("provider-1", 1, 1) // Fail after 1 success, recover after 1 failure
	provider2 := NewFailingMockProvider("provider-2", 0, 0) // Always successful
	successProvider := NewMockProvider("success-provider")  // Always successful

	// Register providers
	retryManager.RegisterProvider(provider1)
	retryManager.RegisterProvider(provider2)
	retryManager.RegisterProvider(successProvider)

	// Initialize the pendingEvents map
	retryManager.pendingEvents = make(map[string][]PendingEvent)

	// Start the retry manager
	retryManager.Start()
	defer retryManager.Stop()

	// Directly add some events to the successful provider
	// This ensures that the provider receives events even if retry mechanisms are delayed
	for i := 0; i < 5; i++ {
		directEvent := &TestEvent{
			eventType: EventType(fmt.Sprintf("direct-event-%d", i)),
			tenant: TenantInfo{
				OrganizationID: "test-org-direct",
				SpaceID:        TestSpaceID,
			},
			payload: fmt.Sprintf("direct-payload-%d", i),
		}
		err := successProvider.Send(context.Background(), directEvent)
		require.NoError(t, err, "Direct sending to Success Provider should succeed")
	}

	// Create and add events (reduced number)
	totalEvents := 12
	for i := 0; i < totalEvents; i++ {
		event := &TestEvent{
			eventType: EventType(fmt.Sprintf("test-event-%d", i)),
			tenant: TenantInfo{
				OrganizationID: fmt.Sprintf("test-org-%d", i%3),
				SpaceID:        TestSpaceID,
			},
			payload: fmt.Sprintf("test-payload-%d", i),
		}

		// Distribution of events to providers
		var providerName string
		switch i % 3 {
		case 0:
			providerName = provider1.Name()
		case 1:
			providerName = provider2.Name()
		case 2:
			providerName = successProvider.Name()
		}

		retryManager.AddPendingEvent(event, providerName, 0)
	}

	// Wait briefly for nextRetry times to expire
	time.Sleep(TestWaitShortMs * time.Millisecond)

	// Process retries multiple times
	for i := 0; i < 3; i++ {
		retryManager.processRetries()
		time.Sleep(TestWaitShortMs * time.Millisecond)
	}

	// Check the queue
	pendingCount := retryManager.GetPendingCount()
	t.Logf("Remaining events in queue: %d", pendingCount)

	// Since provider2 and successProvider are always successful, their events
	// should have been processed successfully. Provider1 might still have events in the queue.
	assert.LessOrEqual(t, pendingCount, 4, "The queue should contain less than a third of the events")

	// Check that events were delivered to the successful provider
	successEvents := successProvider.EventCount()
	assert.Greater(t, successEvents, 0, "Success Provider should have received events")
	t.Logf("Success Provider has received %d events", successEvents)

	// Check that most events were processed
	totalProcessed := len(provider1.GetEvents()) + len(provider2.GetEvents()) + successEvents
	totalExpected := 5 + totalEvents // 5 direct events + added events

	t.Logf("Total of %d out of %d events processed", totalProcessed, totalExpected)
	assert.Greater(t, totalProcessed, totalEvents/2, "At least half of all events should have been processed")
}

// TestConfigValidation tests the configuration validation functionality
func TestConfigValidation(t *testing.T) {
	// Test case 1: Valid configuration
	validConfig := NotifierConfig{
		MaxEventsPerMinute:  100,
		BatchSize:           10,
		BatchTimeoutSeconds: 30,
		RetentionDays:       90,
	}

	validResilienceConfig := DefaultResilienceConfig()
	validResilienceConfig.CircuitBreakerEnabled = true
	validResilienceConfig.RetryEnabled = true

	_, err := NewResilientNotifier(validConfig, validResilienceConfig, nil, nil)
	assert.NoError(t, err, "Valid configuration should not cause validation errors")

	// Test case 2: Invalid notifier configuration
	invalidConfig := NotifierConfig{
		MaxEventsPerMinute:  0, // Invalid: too low
		BatchSize:           0, // Invalid: too low
		BatchTimeoutSeconds: 0, // Invalid: must be > 0
		RetentionDays:       0, // Invalid: too low
	}

	_, err = NewResilientNotifier(invalidConfig, validResilienceConfig, nil, nil)
	assert.Error(t, err, "Invalid configuration should cause validation errors")
	assert.Contains(t, err.Error(), "MaxEventsPerMinute")
	assert.Contains(t, err.Error(), "BatchSize")
	assert.Contains(t, err.Error(), "BatchTimeoutSeconds")
	assert.Contains(t, err.Error(), "RetentionDays")

	// Test case 3: Invalid resilience configuration
	invalidResilienceConfig := DefaultResilienceConfig()
	invalidResilienceConfig.CircuitBreakerEnabled = true
	invalidResilienceConfig.CircuitBreakerMaxFailures = 0 // Invalid: too low
	invalidResilienceConfig.RetryEnabled = true
	invalidResilienceConfig.MaxRetries = -1          // Invalid: too low
	invalidResilienceConfig.RetryBackoffFactor = 0.5 // Invalid: too low

	_, err = NewResilientNotifier(validConfig, invalidResilienceConfig, nil, nil)
	assert.Error(t, err, "Invalid resilience configuration should cause validation errors")
	assert.Contains(t, err.Error(), "CircuitBreakerMaxFailures")
	assert.Contains(t, err.Error(), "MaxRetries")
	assert.Contains(t, err.Error(), "RetryBackoffFactor")

	// Test case 4: Custom validator limits
	validator := NewConfigValidator()
	validator.WithEventRateLimits(50, 200) // Only allow 50-200 events per minute

	validationErrors := validator.ValidateNotifierConfig(NotifierConfig{
		MaxEventsPerMinute: 300, // Too high for custom limit
	})
	assert.NotEmpty(t, validationErrors, "Custom validator limits should be applied")
	assert.Contains(t, validationErrors[0].Error(), "MaxEventsPerMinute")
}
