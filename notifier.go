// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ResilientNotifier erweitert den Notifier um Resilienz-Mechanismen
type ResilientNotifier struct {
	config             NotifierConfig
	resilienceConfig   ResilienceConfig
	originalProviders  []Provider
	resilientProviders map[string]*ProviderWithGoBreakerWrapper
	retryManager       *RetryManager
	dataLake           DataLake
	mu                 sync.RWMutex
	tracer             trace.Tracer
	meter              metric.Meter
}

// NewResilientNotifier erstellt einen neuen Notifier mit Resilienz-Mechanismen
func NewResilientNotifier(config NotifierConfig, resilienceConfig ResilienceConfig, tracer trace.Tracer, meter metric.Meter, providers ...Provider) (*ResilientNotifier, error) {
	// Validate configurations
	validator := NewConfigValidator()
	validationErrors := validator.ValidateConfigs(config, resilienceConfig)
	if len(validationErrors) > 0 {
		errorMessages := ""
		for _, err := range validationErrors {
			errorMessages += err.Error() + "; "
		}
		return nil, fmt.Errorf("invalid configuration: %s", errorMessages)
	}

	rn := &ResilientNotifier{
		config:             config,
		resilienceConfig:   resilienceConfig,
		originalProviders:  make([]Provider, 0, len(providers)),
		resilientProviders: make(map[string]*ProviderWithGoBreakerWrapper),
		tracer:             tracer,
		meter:              meter,
	}

	// Erstelle Retry-Manager, wenn aktiviert
	if resilienceConfig.RetryEnabled {
		rn.retryManager = NewRetryManager(resilienceConfig, tracer, meter)
	}

	// Umhülle alle Provider mit Circuit Breaker
	for _, provider := range providers {
		rn.RegisterProvider(provider)
	}

	return rn, nil
}

// RegisterProvider registriert einen Provider mit Resilienz-Mechanismen
func (rn *ResilientNotifier) RegisterProvider(provider Provider) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	// Füge zum Original-Provider-Array hinzu
	rn.originalProviders = append(rn.originalProviders, provider)

	// Erstelle Circuit Breaker für den Provider
	if rn.resilienceConfig.CircuitBreakerEnabled {
		cb := NewGoBreakerWrapper(
			provider.Name(),
			rn.resilienceConfig.CircuitBreakerMaxFailures,
			rn.resilienceConfig.CircuitBreakerTimeout,
			rn.meter,
		)

		resilientProvider := NewProviderWithGoBreakerWrapper(provider, cb, rn.tracer)
		rn.resilientProviders[provider.Name()] = resilientProvider

		log.Info().
			Str("provider", provider.Name()).
			Int("max_failures", rn.resilienceConfig.CircuitBreakerMaxFailures).
			Msg(LogProviderWithCircuitBreaker)
	}

	// Registriere beim Retry-Manager
	if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
		// Wenn der Provider mit Circuit Breaker umhüllt ist, registriere diesen
		if rn.resilienceConfig.CircuitBreakerEnabled {
			rn.retryManager.RegisterProvider(rn.resilientProviders[provider.Name()])
		} else {
			// Sonst registriere den Original-Provider
			rn.retryManager.RegisterProvider(provider)
		}

		log.Info().
			Str("provider", provider.Name()).
			Int("max_retries", rn.resilienceConfig.MaxRetries).
			Msg("Provider registered with retry manager")
	}
}

// SetDataLake setzt den DataLake für persistente Speicherung
func (rn *ResilientNotifier) SetDataLake(dataLake DataLake) {
	rn.dataLake = dataLake

	// Setze auch im Retry-Manager, wenn aktiv
	if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
		rn.retryManager.SetDataLake(dataLake)
	}

	log.Info().
		Str("datalake", dataLake.Name()).
		Msg("DataLake configured for resilient notification")
}

// Start startet alle Hintergrund-Worker
func (rn *ResilientNotifier) Start() {
	// Starte den Retry-Manager, wenn aktiviert
	if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
		rn.retryManager.Start()
		log.Info().Msg("Retry manager started")
	}
}

// Stop stoppt alle Hintergrund-Worker
func (rn *ResilientNotifier) Stop() {
	// Stoppe den Retry-Manager, wenn aktiviert
	if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
		rn.retryManager.Stop()
		log.Info().Msg("Retry manager stopped")
	}
}

// Notify sendet ein Event an alle registrierten Provider
func (rn *ResilientNotifier) Notify(ctx context.Context, event BaseEvent) {
	eventType := event.GetType()
	tenant := event.GetTenant()

	// Erstelle einen Span für die Benachrichtigung
	ctx, span := rn.tracer.Start(ctx, "ResilientNotifier.Notify")
	defer span.End()

	span.SetAttributes(
		attribute.String("event.type", string(eventType)),
		attribute.String("tenant.organization", tenant.OrganizationID),
		attribute.String("tenant.space", tenant.SpaceID),
	)

	// Datenlake-Speicherung für Audit/Historie
	if rn.dataLake != nil {
		// Hier könnte auch ein DataLake mit Retry-Mechanismus umhüllt werden
		if err := rn.dataLake.Store(ctx, event); err != nil {
			log.Error().
				Err(err).
				Str("event_type", string(eventType)).
				Msg("Failed to store event in data lake")
			span.RecordError(err)
		}
	}

	// Lokale Funktion für die Verarbeitung einzelner Provider
	sendEvent := func(ctx context.Context, providerName string, sendFunc func(context.Context, BaseEvent) error, spanName string, extraAttrs ...attribute.KeyValue) {
		providerCtx, providerSpan := rn.tracer.Start(ctx, spanName,
			trace.WithAttributes(append([]attribute.KeyValue{
				attribute.String("provider", providerName),
			}, extraAttrs...)...),
		)
		defer providerSpan.End()

		err := sendFunc(providerCtx, event)

		if err != nil {
			log.Warn().
				Err(err).
				Str("provider", providerName).
				Str("event_type", string(eventType)).
				Msg("Failed to send notification")

			providerSpan.RecordError(err)
			providerSpan.SetStatus(codes.Error, err.Error())

			// Füge für erneute Versuche hinzu, wenn Retry aktiviert ist
			if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
				rn.retryManager.AddPendingEvent(event, providerName, 0)
			}
		} else {
			providerSpan.SetStatus(codes.Ok, "")
		}
	}

	// Verarbeitung basierend auf Circuit Breaker Konfiguration
	if rn.resilienceConfig.CircuitBreakerEnabled {
		rn.mu.RLock()
		providers := make([]*ProviderWithGoBreakerWrapper, 0, len(rn.resilientProviders))
		for _, provider := range rn.resilientProviders {
			providers = append(providers, provider)
		}
		rn.mu.RUnlock()

		for _, provider := range providers {
			providerName := provider.Name()
			sendEvent(ctx, providerName,
				provider.Send,
				"ResilientProvider.Send",
				attribute.String("circuit_state", provider.GetState().String()),
			)
		}
	} else {
		// Ohne Circuit Breaker
		rn.mu.RLock()
		providers := make([]Provider, len(rn.originalProviders))
		copy(providers, rn.originalProviders)
		rn.mu.RUnlock()

		for _, provider := range providers {
			providerName := provider.Name()
			sendEvent(ctx, providerName, provider.Send, "Provider.Send")
		}
	}
}

// GetCircuitBreakerStatus gibt einen Statusbericht über die Circuit Breaker zurück
func (rn *ResilientNotifier) GetCircuitBreakerStatus() map[string]string {
	if !rn.resilienceConfig.CircuitBreakerEnabled {
		return map[string]string{
			"status": "disabled",
		}
	}

	rn.mu.RLock()
	defer rn.mu.RUnlock()

	status := make(map[string]string)
	for name, provider := range rn.resilientProviders {
		status[name] = provider.GetState().String()
	}
	return status
}

// GetRetryStatus gibt einen Statusbericht über die Retry-Warteschlange zurück
func (rn *ResilientNotifier) GetRetryStatus() map[string]interface{} {
	if !rn.resilienceConfig.RetryEnabled || rn.retryManager == nil {
		return map[string]interface{}{"enabled": false}
	}

	result := map[string]interface{}{
		"enabled":       true,
		"pending_count": rn.retryManager.GetPendingCount(),
		"by_provider":   rn.retryManager.GetPendingCountByProvider(),
	}

	return result
}

// ResetCircuitBreaker setzt einen Circuit Breaker für einen bestimmten Provider zurück
func (rn *ResilientNotifier) ResetCircuitBreaker(providerName string) bool {
	if !rn.resilienceConfig.CircuitBreakerEnabled {
		return false
	}

	rn.mu.RLock()
	provider, exists := rn.resilientProviders[providerName]
	rn.mu.RUnlock()

	if !exists {
		return false
	}

	provider.ResetCircuitBreaker()
	return true
}

// ResetAllCircuitBreakers setzt alle Circuit Breaker zurück
func (rn *ResilientNotifier) ResetAllCircuitBreakers() {
	if !rn.resilienceConfig.CircuitBreakerEnabled {
		return
	}

	rn.mu.RLock()
	providers := make([]*ProviderWithGoBreakerWrapper, 0, len(rn.resilientProviders))
	for _, provider := range rn.resilientProviders {
		providers = append(providers, provider)
	}
	rn.mu.RUnlock()

	for _, provider := range providers {
		provider.ResetCircuitBreaker()
	}
	log.Info().Msg(LogAllCircuitBreakersReset)
}

// NewResilientNotifierWithPool creates a notifier that uses a provider pool for improved resource management
func NewResilientNotifierWithPool(
	config NotifierConfig,
	resilienceConfig ResilienceConfig,
	tracer trace.Tracer,
	meter metric.Meter,
	pool *ProviderPool,
	providerTypes ...string) (*ResilientNotifier, error) {

	// Validate configurations
	validator := NewConfigValidator()
	validationErrors := validator.ValidateConfigs(config, resilienceConfig)
	if len(validationErrors) > 0 {
		errorMessages := ""
		for _, err := range validationErrors {
			errorMessages += err.Error() + "; "
		}
		return nil, fmt.Errorf("invalid configuration: %s", errorMessages)
	}

	// Check if we have at least one provider type
	if len(providerTypes) == 0 {
		return nil, fmt.Errorf("at least one provider type must be specified")
	}

	// Check that the pool has registered factories for all provider types
	for _, providerType := range providerTypes {
		_, err := pool.GetProvider(context.Background(), providerType)
		if err != nil {
			return nil, fmt.Errorf("provider pool has no factory for provider type '%s': %w", providerType, err)
		}
		// Return the provider to the pool
		provider, _ := pool.GetProvider(context.Background(), providerType)
		pool.ReleaseProvider(providerType, provider)
	}

	rn := &ResilientNotifier{
		config:             config,
		resilienceConfig:   resilienceConfig,
		originalProviders:  make([]Provider, 0),
		resilientProviders: make(map[string]*ProviderWithGoBreakerWrapper),
		tracer:             tracer,
		meter:              meter,
	}

	// Create Retry-Manager if enabled
	if resilienceConfig.RetryEnabled {
		rn.retryManager = NewRetryManager(resilienceConfig, tracer, meter)
	}

	// Create a provider adapter for each provider type that wraps the pool
	for _, providerType := range providerTypes {
		// Create a pooled provider adapter that fetches and returns providers from the pool
		pooledProvider := NewPooledProviderAdapter(providerType, pool)
		rn.RegisterProvider(pooledProvider)

		log.Info().
			Str("provider_type", providerType).
			Msg("Registered pooled provider adapter")
	}

	return rn, nil
}
