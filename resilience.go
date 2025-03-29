// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ResilienceConfig contains configuration options for resilience mechanisms
type ResilienceConfig struct {
	// Circuit Breaker configuration
	CircuitBreakerEnabled     bool
	CircuitBreakerMaxFailures int
	CircuitBreakerTimeout     time.Duration
	// Retry configuration
	RetryEnabled       bool
	MaxRetries         int
	RetryInitialDelay  time.Duration
	RetryMaxDelay      time.Duration
	RetryBackoffFactor float64
	// Whether to persist events that have exhausted all retry attempts
	PersistFailedEvents bool
}

// DefaultResilienceConfig returns a default configuration for resilience mechanisms
func DefaultResilienceConfig() ResilienceConfig {
	return ResilienceConfig{
		CircuitBreakerEnabled:     false,
		CircuitBreakerMaxFailures: DefaultCircuitBreakerMaxFailures,
		CircuitBreakerTimeout:     DefaultCircuitBreakerTimeoutSec * time.Second,
		RetryEnabled:              false,
		MaxRetries:                DefaultMaxRetries,
		RetryInitialDelay:         DefaultRetryInitialDelaySec * time.Second,
		RetryMaxDelay:             DefaultRetryMaxDelaySec * time.Second,
		RetryBackoffFactor:        DefaultRetryBackoffFactor,
		PersistFailedEvents:       false,
	}
}

// CircuitBreakerState represents the possible states of a CircuitBreaker
type CircuitBreakerState int

const (
	// CircuitClosed is the normal state, allows requests
	CircuitClosed CircuitBreakerState = iota
	// CircuitOpen is the tripped state, blocks all requests
	CircuitOpen
	// CircuitHalfOpen is the testing state, allows limited test requests
	CircuitHalfOpen
)

// CircuitClosedStr is the string representation of the CircuitClosed state
const CircuitClosedStr = "CLOSED"

// CircuitOpenStr is the string representation of the CircuitOpen state
const CircuitOpenStr = "OPEN"

// CircuitHalfOpenStr is the string representation of the CircuitHalfOpen state
const CircuitHalfOpenStr = "HALF_OPEN"

// CircuitUnknownStr is the string representation of an unknown state
const CircuitUnknownStr = "UNKNOWN"

// String returns a string representation of the CircuitBreakerState
func (s CircuitBreakerState) String() string {
	switch s {
	case CircuitClosed:
		return CircuitClosedStr
	case CircuitOpen:
		return CircuitOpenStr
	case CircuitHalfOpen:
		return CircuitHalfOpenStr
	default:
		return CircuitUnknownStr
	}
}

// PendingEvent represents an event that is pending retry
type PendingEvent struct {
	Event        BaseEvent
	RetryCount   int
	NextRetry    time.Time
	ProviderName string
}

// RetryManager manages retry attempts for failed events
type RetryManager struct {
	config        ResilienceConfig
	pendingEvents map[string][]PendingEvent // Organized by tenant
	providers     map[string]Provider
	dataLake      DataLake
	tracer        trace.Tracer
	wg            sync.WaitGroup
	stopChan      chan struct{}
	mu            sync.RWMutex
	providersMu   sync.RWMutex
	retryCounter  metric.Int64Counter
}

// NewRetryManager creates a new retry manager
func NewRetryManager(config ResilienceConfig, tracer trace.Tracer, meter metric.Meter) *RetryManager {
	rm := &RetryManager{
		config:        config,
		pendingEvents: make(map[string][]PendingEvent),
		providers:     make(map[string]Provider),
		tracer:        tracer,
		stopChan:      make(chan struct{}),
	}

	// Initialize metrics if meter is provided
	if meter != nil {
		retryCounter, _ := meter.Int64Counter(
			"notifier.retry.count",
			metric.WithDescription("Number of retry operations"),
		)
		rm.retryCounter = retryCounter
	}

	return rm
}

// RegisterProvider registers a provider with the retry manager
func (rm *RetryManager) RegisterProvider(provider Provider) {
	rm.providersMu.Lock()
	defer rm.providersMu.Unlock()

	rm.providers[provider.Name()] = provider
}

// SetDataLake sets the DataLake for persisting events
func (rm *RetryManager) SetDataLake(dataLake DataLake) {
	rm.dataLake = dataLake
}

// Start begins processing retry attempts
func (rm *RetryManager) Start() {
	rm.wg.Add(1)
	go rm.processingLoop()
}

// Stop stops the retry manager
func (rm *RetryManager) Stop() {
	close(rm.stopChan)
	rm.wg.Wait()
}

// AddPendingEvent adds an event to the retry queue
func (rm *RetryManager) AddPendingEvent(event BaseEvent, providerName string, retryCount int) {
	// Check if we've reached the max retries
	if retryCount >= rm.config.MaxRetries {
		log.Warn().
			Str("provider", providerName).
			Str("event_type", string(event.GetType())).
			Int("retry_count", retryCount).
			Msg(LogMaxRetriesExceeded)

		// Persist the event in the DataLake if configured
		if rm.config.PersistFailedEvents && rm.dataLake != nil {
			err := rm.dataLake.Store(context.Background(), event)
			if err != nil {
				log.Error().
					Err(err).
					Str("provider", providerName).
					Str("event_type", string(event.GetType())).
					Msg(LogFailedToPersist)
			} else {
				log.Info().
					Str("provider", providerName).
					Str("event_type", string(event.GetType())).
					Msg(LogEventPersisted)
			}
		}
		return
	}

	rm.mu.Lock()
	defer rm.mu.Unlock()

	// Calculate next retry time with exponential backoff strategy
	delay := rm.calculateBackoff(retryCount)
	nextRetry := time.Now().Add(delay)

	tenant := event.GetTenant()
	tenantKey := tenant.OrganizationID
	if tenantKey == "" {
		tenantKey = "global"
	}

	pending := PendingEvent{
		Event:        event,
		RetryCount:   retryCount + 1,
		NextRetry:    nextRetry,
		ProviderName: providerName,
	}

	rm.pendingEvents[tenantKey] = append(rm.pendingEvents[tenantKey], pending)

	log.Info().
		Str("provider", providerName).
		Str("event_type", string(event.GetType())).
		Int("retry_count", retryCount+1).
		Time("next_retry", nextRetry).
		Msg(LogEventAddedToRetryQueue)

	// Record OTel metric for retries
	if rm.retryCounter != nil {
		rm.retryCounter.Add(context.Background(), 1, metric.WithAttributes(
			attribute.String("provider", providerName),
			attribute.String("event_type", string(event.GetType())),
			attribute.String("action", "queued"),
		))
	}
}

// calculateBackoff calculates the wait time for the next retry attempt
func (rm *RetryManager) calculateBackoff(retryCount int) time.Duration {
	// Exponential backoff with jitter
	backoff := float64(rm.config.RetryInitialDelay) * math.Pow(rm.config.RetryBackoffFactor, float64(retryCount))

	// Add random jitter (±20%)
	jitter := (rand.Float64()*0.4 - 0.2) * backoff
	backoff += jitter

	// Ensure we don't exceed the maximum delay
	if backoff > float64(rm.config.RetryMaxDelay) {
		backoff = float64(rm.config.RetryMaxDelay)
	}

	return time.Duration(backoff)
}

// processingLoop performs regular retry processing
func (rm *RetryManager) processingLoop() {
	defer rm.wg.Done()

	ticker := time.NewTicker(ProcessingRetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rm.processRetries()
		case <-rm.stopChan:
			// Process all remaining retries before exiting
			rm.processRetries()
			return
		}
	}
}

// processRetries processes all pending retry attempts
func (rm *RetryManager) processRetries() {
	now := time.Now()

	rm.mu.Lock()

	// Collect events that need to be retried now
	var eventsToRetry []PendingEvent
	var newPendingMap = make(map[string][]PendingEvent)

	for tenantKey, events := range rm.pendingEvents {
		var remaining []PendingEvent

		for _, pe := range events {
			if now.After(pe.NextRetry) {
				eventsToRetry = append(eventsToRetry, pe)
			} else {
				remaining = append(remaining, pe)
			}
		}

		if len(remaining) > 0 {
			newPendingMap[tenantKey] = remaining
		}
	}

	// Update the pending map
	rm.pendingEvents = newPendingMap
	rm.mu.Unlock()

	// Nothing to do if there are no events to retry
	if len(eventsToRetry) == 0 {
		return
	}

	log.Debug().Int("event_count", len(eventsToRetry)).Msg(LogProcessingRetryEvents)

	// Process the events that need to be retried
	for _, pe := range eventsToRetry {
		ctx := context.Background()

		if rm.tracer != nil {
			var span trace.Span
			ctx, span = rm.tracer.Start(ctx, "RetryManager.RetryEvent")
			span.SetAttributes(
				attribute.String("event_type", string(pe.Event.GetType())),
				attribute.String("provider", pe.ProviderName),
				attribute.Int("retry_count", pe.RetryCount),
			)
			defer span.End()
		}

		// Find the matching provider
		rm.providersMu.RLock()
		provider, exists := rm.providers[pe.ProviderName]
		rm.providersMu.RUnlock()

		if !exists {
			log.Warn().
				Str("provider", pe.ProviderName).
				Str("event_type", string(pe.Event.GetType())).
				Msg(LogProviderNotFoundForRetry)

			// Persist event in DataLake, if configured
			if rm.config.PersistFailedEvents && rm.dataLake != nil {
				if err := rm.dataLake.Store(ctx, pe.Event); err != nil {
					log.Error().
						Err(err).
						Str("provider", pe.ProviderName).
						Str("event_type", string(pe.Event.GetType())).
						Msg(LogFailedToPersist)
				}
			}
			continue
		}

		// Try to send the event again
		log.Info().
			Str("provider", pe.ProviderName).
			Str("event_type", string(pe.Event.GetType())).
			Int("retry_count", pe.RetryCount).
			Msg("Attempting to retry event")

		if rm.retryCounter != nil {
			rm.retryCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("provider", pe.ProviderName),
				attribute.String("event_type", string(pe.Event.GetType())),
				attribute.String("action", "attempt"),
				attribute.Int("retry_count", pe.RetryCount),
			))
		}

		err := provider.Send(ctx, pe.Event)

		if err != nil {
			log.Warn().
				Err(err).
				Str("provider", pe.ProviderName).
				Str("event_type", string(pe.Event.GetType())).
				Int("retry_count", pe.RetryCount).
				Msg(LogRetryFailed)

			// Plan another retry if the limit has not been reached
			rm.AddPendingEvent(pe.Event, pe.ProviderName, pe.RetryCount)

			if rm.retryCounter != nil {
				rm.retryCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("provider", pe.ProviderName),
					attribute.String("event_type", string(pe.Event.GetType())),
					attribute.String("action", "failed"),
					attribute.Int("retry_count", pe.RetryCount),
				))
			}
		} else {
			log.Info().
				Str("provider", pe.ProviderName).
				Str("event_type", string(pe.Event.GetType())).
				Int("retry_count", pe.RetryCount).
				Msg(LogRetrySucceeded)

			if rm.retryCounter != nil {
				rm.retryCounter.Add(ctx, 1, metric.WithAttributes(
					attribute.String("provider", pe.ProviderName),
					attribute.String("event_type", string(pe.Event.GetType())),
					attribute.String("action", "success"),
					attribute.Int("retry_count", pe.RetryCount),
				))
			}
		}
	}
}

// GetPendingCount returns the number of pending retry attempts
func (rm *RetryManager) GetPendingCount() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	count := 0
	for _, events := range rm.pendingEvents {
		count += len(events)
	}

	return count
}

// GetPendingCountByProvider returns the number of pending retry attempts per provider
func (rm *RetryManager) GetPendingCountByProvider() map[string]int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	result := make(map[string]int)

	for _, events := range rm.pendingEvents {
		for _, pe := range events {
			result[pe.ProviderName]++
		}
	}

	return result
}
