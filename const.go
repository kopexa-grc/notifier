// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"fmt"
	"time"
)

// EventType represents the type of notification event
type EventType string

// Event types
const (
	EventTypeInfo       EventType = "info"
	EventTypeWarning    EventType = "warning"
	EventTypeCritical   EventType = "critical"
	EventTypeEmergency  EventType = "emergency"
	EventTypeCompletion EventType = "completion"
	EventTypeUpdate     EventType = "update"
	EventTypeTest       EventType = "test"
)

const (
	// Document events
	EventTypeDocumentCreated EventType = "document.created"
	EventTypeDocumentUpdated EventType = "document.updated"
	EventTypeDocumentDeleted EventType = "document.deleted"

	// Action events
	EventTypeActionCreated EventType = "action.created"
	EventTypeActionUpdated EventType = "action.updated"
	EventTypeActionDeleted EventType = "action.deleted"
)

// EventType-Konstanten und andere Deklarationen beibehalten
const (
	// DocumentType definiert einen Dokumenttyp
	DocumentTypeContract = "contract"
	DocumentTypePolicy   = "policy"
	DocumentTypeOther    = "other"
)

// Log constants for notifier
const (
	LogInitNotifier                     = "Notifier initialized with enterprise features"
	LogEventSent                        = "Event sent successfully"
	LogEventRateLimited                 = "Event rate limited"
	LogEventBatchProcessed              = "Batch of events processed"
	LogProviderRegistered               = "Provider registered with notifier"
	LogProviderUnregistered             = "Provider unregistered from notifier"
	LogDataLakeConfigured               = "Data lake configured for notifier"
	LogRetentionPolicyApplied           = "Retention policy applied to event data"
	LogBatchingEnabled                  = "Batch processing enabled"
	LogBatchingDisabled                 = "Batch processing disabled"
	LogEventStored                      = "Event stored in data lake"
	LogRateLimitUpdated                 = "Rate limit updated"
	LogCircuitBreakerTripped            = "Circuit breaker tripped for provider"
	LogCircuitBreakerReset              = "Circuit breaker reset for provider"
	LogCircuitBreakerHalfOpen           = "Circuit breaker half-open for provider"
	LogCircuitBreakerClosed             = "Circuit breaker closed for provider"
	LogRetryScheduled                   = "Event retry scheduled"
	LogRetrySuccessful                  = "Event retry successful"
	LogRetrySucceeded                   = "Event retry succeeded"
	LogRetryFailed                      = "Event retry failed"
	LogRetryMaxAttemptsReached          = "Maximum retry attempts reached"
	LogCircuitBreakerTrippedOpen        = "Circuit breaker tripped open"
	LogCircuitBreakerReopened           = "Circuit breaker reopened after failed test request"
	LogCircuitBreakerClosedAfterSuccess = "Circuit breaker closed after successful test request"
	LogCircuitBreakerManuallyReset      = "Circuit breaker manually reset"
	LogAllCircuitBreakersReset          = "All circuit breakers have been reset"
	LogEventDroppedRateLimiting         = "Event dropped due to rate limiting"
	LogEventAddedToRetryQueue           = "Event added to retry queue"
	LogMaxRetriesExceeded               = "Max retries exceeded, event will be persisted to datalake"
	LogEventPersisted                   = "Event persisted to datalake after max retries"
	LogFailedToPersist                  = "Failed to persist event to datalake"
	LogProviderNotFoundForRetry         = "Provider not found for retry, event will be persisted to datalake"
	LogDataLakeEnabled                  = "Data lake storage enabled for notifications"
	LogProcessingRetryEvents            = "Processing retry events"
	LogProviderWithCircuitBreaker       = "Provider registered with circuit breaker"
	LogProviderWithRetryManager         = "Provider registered with retry manager"
	LogStartingBatchWorkers             = "Starting batch processing workers"
	LogBatchWorkerStarted               = "Batch worker started"
	LogProcessingBatch                  = "Processing batch of events"
	LogRetentionPolicyUpdated           = "Retention policy updated"
	LogPurgingOldEvents                 = "Purging old events"
	LogPurgeSuccess                     = "Successfully purged old events"
	LogFailedToRegisterMetric           = "Failed to register metric"
	LogCircuitBreakerCreated            = "Circuit breaker created"
	LogCircuitBreakerRetripped          = "Circuit breaker retripped back to open after failed test request"
)

// Default constants for the notifier
const (
	// General defaults
	DefaultBatchSize           = 10
	DefaultBatchTimeoutSeconds = 30
	DefaultRetentionDays       = 90
	DefaultMaxEventsPerMinute  = 100

	// Retry defaults
	DefaultMaxRetries        = 3
	DefaultRetryInitialDelay = 5 * time.Second
	DefaultRetryMaxDelay     = 5 * time.Minute
	DefaultRetryDelayFactor  = 2.0

	// Circuit breaker defaults
	DefaultCircuitTimeout   = 60 * time.Second
	DefaultSuccessThreshold = 5
	DefaultFailureThreshold = 3

	// Additional circuit breaker defaults
	DefaultCircuitBreakerMaxFailures = 5
	DefaultCircuitBreakerTimeoutSec  = 60

	// Additional retry defaults
	DefaultRetryInitialDelaySec = 5
	DefaultRetryMaxDelaySec     = 300
	DefaultRetryBackoffFactor   = 2.0
	ProcessingRetryInterval     = 30 * time.Second

	// Worker and queue defaults
	DefaultWorkerCount = 3
	DefaultBucketSize  = 5
)

// Error constants
const (
	ErrNotifierStopped  = "notifier has been stopped"
	ErrProviderNotFound = "provider not found"
	ErrCircuitOpen      = "circuit breaker is open"
	ErrSimulatedFailure = "simulated failure"
)

// Error variables
var (
	// ErrProviderNotRegistered is returned when trying to use a provider type that hasn't been registered
	ErrProviderNotRegistered = fmt.Errorf("provider type not registered with pool")
)

// Test constants
const (
	TestOrgID        = "test-org-id"
	TestSpaceID      = "test-space-id"
	TestEventType    = "test-event"
	TestEventMessage = "This is a test message"

	// Test constants for resilience testing
	TestProviderName               = "test-provider"
	TestCircuitBreakerFailures     = 3
	TestCircuitBreakerTimeoutMs    = 1000
	TestResilientNotifierTimeoutMs = 2000
	TestRetryInitialDelayMs        = 100
	TestRetryMaxDelayMs            = 5000
	TestWaitIntervalMs             = 200
	TestWaitShortMs                = 50
	TestConcurrentRetryDelayMs     = 300
)
