// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
)

// Service is the main service for notification functionality
type Service struct {
	notifier          *Notifier
	resilientNotifier *ResilientNotifier
}

// NewService creates a new instance of the Notifier service
func NewService(o ...Option) (*Service, error) {
	opts := defaultOptions()

	for _, opt := range o {
		opt(opts)
	}

	// Create notifier with enterprise configuration
	notifierConfig := NotifierConfig{
		MaxEventsPerMinute:  opts.maxEventsPerMinute,
		BatchSize:           opts.batchSize,
		BatchTimeoutSeconds: opts.batchTimeoutSeconds,
		RetentionDays:       opts.retentionDays,
	}

	// Erstelle Resilienz-Konfiguration, wenn aktiviert
	var resilientNotifier *ResilientNotifier
	var err error

	if opts.circuitBreakerEnabled || opts.retryEnabled {
		// Verwende den resilienten Notifier
		resilienceConfig := ResilienceConfig{
			CircuitBreakerEnabled:     opts.circuitBreakerEnabled,
			CircuitBreakerMaxFailures: opts.circuitBreakerMaxFailures,
			CircuitBreakerTimeout:     time.Duration(opts.circuitBreakerTimeoutSec) * time.Second,
			RetryEnabled:              opts.retryEnabled,
			MaxRetries:                opts.maxRetries,
			RetryInitialDelay:         time.Duration(opts.retryInitialDelaySec) * time.Second,
			RetryMaxDelay:             time.Duration(opts.retryMaxDelaySec) * time.Second,
			RetryBackoffFactor:        opts.retryBackoffFactor,
			PersistFailedEvents:       opts.persistFailedEvents,
		}

		// Standardwerte setzen, wenn nicht konfiguriert
		if opts.circuitBreakerEnabled && opts.circuitBreakerMaxFailures <= 0 {
			resilienceConfig.CircuitBreakerMaxFailures = 5
		}
		if opts.circuitBreakerEnabled && opts.circuitBreakerTimeoutSec <= 0 {
			resilienceConfig.CircuitBreakerTimeout = 60 * time.Second
		}
		if opts.retryEnabled && opts.maxRetries <= 0 {
			resilienceConfig.MaxRetries = 3
		}
		if opts.retryEnabled && opts.retryInitialDelaySec <= 0 {
			resilienceConfig.RetryInitialDelay = 1 * time.Second
		}
		if opts.retryEnabled && opts.retryMaxDelaySec <= 0 {
			resilienceConfig.RetryMaxDelay = 30 * time.Second
		}
		if opts.retryEnabled && opts.retryBackoffFactor <= 0 {
			resilienceConfig.RetryBackoffFactor = 2.0
		}

		// OTel-Tracer und Meter
		tracer := otel.Tracer("github.com/kopexa-grc/kopexa/notifier")
		meter := otel.Meter("github.com/kopexa-grc/kopexa/notifier")

		// Erstelle einen resilienten Notifier
		resilientNotifier, err = NewResilientNotifier(notifierConfig, resilienceConfig, tracer, meter)
		if err != nil {
			return nil, fmt.Errorf("failed to create resilient notifier: %w", err)
		}

		log.Info().
			Bool("circuit_breaker_enabled", opts.circuitBreakerEnabled).
			Bool("retry_enabled", opts.retryEnabled).
			Msg("Resilient notifier created with advanced error handling")
	}

	var notifier *Notifier
	if resilientNotifier == nil {
		// Standard-Notifier ohne Resilienz-Mechanismen verwenden
		notifier = NewNotifier(notifierConfig)
	}

	service := &Service{}

	for _, provider := range opts.providers {
		if resilientNotifier != nil {
			resilientNotifier.RegisterProvider(provider)
		} else {
			notifier.RegisterProvider(provider)
		}
	}

	// Configure data lake if enabled
	if opts.datalake != nil {
		if resilientNotifier != nil {
			resilientNotifier.SetDataLake(opts.datalake)
		} else {
			notifier.SetDataLake(opts.datalake)
		}
		log.Info().Msg("DataLake storage enabled for notifications")
	}

	// Starte den resilienten Notifier, wenn verwendet
	if resilientNotifier != nil {
		resilientNotifier.Start()
		service.resilientNotifier = resilientNotifier
	} else {
		service.notifier = notifier
	}

	return service, nil
}

// Notify allows sending any custom event with a specific payload type
func (s *Service) Notify(ctx context.Context, event BaseEvent) {
	if s.resilientNotifier != nil {
		s.resilientNotifier.Notify(ctx, event)
	} else {
		s.notifier.Notify(ctx, event)
	}
}

// CreateEvent creates a new event with tenant information
func (s *Service) CreateEvent(
	eventType EventType,
	payload interface{},
	userIDs []string,
	organizationID string,
	spaceID string,
) BaseEvent {
	return &Event[interface{}]{
		Type:      eventType,
		Timestamp: time.Now(),
		Payload:   payload,
		UserIDs:   userIDs,
		Tenant: TenantInfo{
			OrganizationID: organizationID,
			SpaceID:        spaceID,
		},
	}
}

// NotifyOrganization sends a notification to all users in an organization
func (s *Service) NotifyOrganization(
	ctx context.Context,
	eventType EventType,
	payload interface{},
	organizationID string,
	userIDs []string,
) {
	event := s.CreateEvent(eventType, payload, userIDs, organizationID, "")
	if s.resilientNotifier != nil {
		s.resilientNotifier.Notify(ctx, event)
	} else {
		s.notifier.Notify(ctx, event)
	}
}

// NotifySpace sends a notification to all users in a space
func (s *Service) NotifySpace(
	ctx context.Context,
	eventType EventType,
	payload interface{},
	organizationID string,
	spaceID string,
	userIDs []string,
) {
	event := s.CreateEvent(eventType, payload, userIDs, organizationID, spaceID)
	if s.resilientNotifier != nil {
		s.resilientNotifier.Notify(ctx, event)
	} else {
		s.notifier.Notify(ctx, event)
	}
}

// QueryOrganizationEvents retrieves events for a specific organization
func (s *Service) QueryOrganizationEvents(
	ctx context.Context,
	organizationID string,
	eventTypes []EventType,
	userID string,
	limit int,
	offset int,
) ([]StoredEvent, error) {
	var dataLake DataLake

	if s.resilientNotifier != nil {
		dataLake = s.resilientNotifier.dataLake
	} else if s.notifier != nil {
		dataLake = s.notifier.dataLake
	}

	if dataLake == nil {
		return nil, fmt.Errorf("data lake storage is not configured")
	}

	query := DataLakeQuery{
		OrganizationID: organizationID,
		EventTypes:     eventTypes,
		UserID:         userID,
		Limit:          limit,
		Offset:         offset,
	}

	return dataLake.GetEvents(ctx, query)
}

// QuerySpaceEvents retrieves events for a specific space
func (s *Service) QuerySpaceEvents(
	ctx context.Context,
	organizationID string,
	spaceID string,
	eventTypes []EventType,
	userID string,
	limit int,
	offset int,
) ([]StoredEvent, error) {
	var dataLake DataLake

	if s.resilientNotifier != nil {
		dataLake = s.resilientNotifier.dataLake
	} else if s.notifier != nil {
		dataLake = s.notifier.dataLake
	}

	if dataLake == nil {
		return nil, fmt.Errorf("data lake storage is not configured")
	}

	query := DataLakeQuery{
		OrganizationID: organizationID,
		SpaceID:        spaceID,
		EventTypes:     eventTypes,
		UserID:         userID,
		Limit:          limit,
		Offset:         offset,
	}

	return dataLake.GetEvents(ctx, query)
}

// GetNotifier returns the internal notifier if direct access is needed
func (s *Service) GetNotifier() *Notifier {
	return s.notifier
}

// GetEventStats returns analytics data for all event types
func (s *Service) GetEventStats() map[EventType]*EventStats {
	if s.resilientNotifier != nil {
		// In der resilienten Implementierung müssten wir die Statistiken noch implementieren
		return make(map[EventType]*EventStats)
	} else if s.notifier != nil {
		return s.notifier.GetEventStats()
	}
	return make(map[EventType]*EventStats)
}

// SetRateLimits updates the rate limiting configuration
func (s *Service) SetRateLimits(perMinute, perHour int) {
	if s.resilientNotifier != nil {
		return
	}

	if s.notifier != nil {
		s.notifier.SetMaxEventsPerMinute(perMinute)
	}
}

// EnableBatching turns on or off event batching
func (s *Service) EnableBatching(enabled bool) {
	if s.resilientNotifier != nil {
		// In der resilienten Implementierung müssten wir diese Methode noch implementieren
	} else if s.notifier != nil {
		s.notifier.EnableBatching(enabled)
	}
}

// SetRetentionDays sets the data retention period in days
func (s *Service) SetRetentionDays(days int) {
	if s.resilientNotifier != nil {
		// In der resilienten Implementierung müssten wir diese Methode noch implementieren
	} else if s.notifier != nil {
		s.notifier.SetRetentionDays(days)
	}
}

// GetNotifierHealth returns health metrics about the notification system
func (s *Service) GetNotifierHealth() map[string]interface{} {
	stats := s.GetEventStats()
	health := make(map[string]interface{})

	// Calculate success rates for each event type
	eventHealthMetrics := make(map[EventType]map[string]interface{})
	for eventType, stat := range stats {
		successRate := 0.0
		if stat.TotalCount > 0 {
			successRate = float64(stat.SuccessCount) / float64(stat.TotalCount) * 100.0
		}

		eventHealthMetrics[eventType] = map[string]interface{}{
			"success_rate":    successRate,
			"total_count":     stat.TotalCount,
			"average_time_ms": stat.AverageTimeMs,
			"last_processed":  stat.LastProcessed,
		}
	}

	health["event_metrics"] = eventHealthMetrics

	if s.resilientNotifier != nil {
		health["provider_count"] = len(s.resilientNotifier.originalProviders)
		health["datalake_enabled"] = s.resilientNotifier.dataLake != nil

		// Füge Resilienz-Metriken hinzu
		health["circuit_breaker_status"] = s.resilientNotifier.GetCircuitBreakerStatus()
		health["retry_status"] = s.resilientNotifier.GetRetryStatus()
	} else if s.notifier != nil {
		health["provider_count"] = len(s.notifier.providers)
		health["datalake_enabled"] = s.notifier.dataLake != nil
		health["batching_enabled"] = s.notifier.batching.enabled
	}

	return health
}

// ResetCircuitBreaker setzt einen Circuit Breaker für einen bestimmten Provider zurück
func (s *Service) ResetCircuitBreaker(providerName string) bool {
	if s.resilientNotifier != nil {
		return s.resilientNotifier.ResetCircuitBreaker(providerName)
	}
	return false
}

// ResetAllCircuitBreakers setzt alle Circuit Breaker zurück
func (s *Service) ResetAllCircuitBreakers() {
	if s.resilientNotifier != nil {
		s.resilientNotifier.ResetAllCircuitBreakers()
	}
}

// Close schließt den Service und alle zugehörigen Ressourcen
func (s *Service) Close() {
	if s.resilientNotifier != nil {
		s.resilientNotifier.Stop()
	}
}
