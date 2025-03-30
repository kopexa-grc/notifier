// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// ResilientNotifier extends the Notifier with resilience mechanisms
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

// NewResilientNotifier creates a new Notifier with resilience mechanisms
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

	// If tracer or meter are nil, use OpenTelemetry defaults
	if tracer == nil {
		tracer = otel.Tracer("github.com/kopexa-grc/notifier")
	}

	if meter == nil {
		meter = otel.Meter("github.com/kopexa-grc/notifier")
	}

	rn := &ResilientNotifier{
		config:             config,
		resilienceConfig:   resilienceConfig,
		originalProviders:  make([]Provider, 0, len(providers)),
		resilientProviders: make(map[string]*ProviderWithGoBreakerWrapper),
		tracer:             tracer,
		meter:              meter,
	}

	// Create Retry Manager if enabled
	if resilienceConfig.RetryEnabled {
		rn.retryManager = NewRetryManager(resilienceConfig, tracer, meter)
	}

	// Wrap all providers with Circuit Breaker
	for _, provider := range providers {
		rn.RegisterProvider(provider)
	}

	return rn, nil
}

// RegisterProvider registers a provider with resilience mechanisms
func (rn *ResilientNotifier) RegisterProvider(provider Provider) {
	rn.mu.Lock()
	defer rn.mu.Unlock()

	// Add to original provider array
	rn.originalProviders = append(rn.originalProviders, provider)

	// Create Circuit Breaker for the provider
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

	// Register with Retry Manager
	if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
		// If the provider is wrapped with Circuit Breaker, register that
		if rn.resilienceConfig.CircuitBreakerEnabled {
			rn.retryManager.RegisterProvider(rn.resilientProviders[provider.Name()])
		} else {
			// Otherwise register the original provider
			rn.retryManager.RegisterProvider(provider)
		}

		log.Info().
			Str("provider", provider.Name()).
			Int("max_retries", rn.resilienceConfig.MaxRetries).
			Msg("Provider registered with retry manager")
	}
}

// SetDataLake sets the DataLake for persistent storage
func (rn *ResilientNotifier) SetDataLake(dataLake DataLake) {
	rn.dataLake = dataLake

	// Also set in Retry Manager if active
	if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
		rn.retryManager.SetDataLake(dataLake)
	}

	log.Info().
		Str("datalake", dataLake.Name()).
		Msg("DataLake configured for resilient notification")
}

// Start starts all background workers
func (rn *ResilientNotifier) Start() {
	// Start the Retry Manager if enabled
	if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
		rn.retryManager.Start()
		log.Info().Msg("Retry manager started")
	}
}

// Stop stops all background workers
func (rn *ResilientNotifier) Stop() {
	// Stop the Retry Manager if enabled
	if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
		rn.retryManager.Stop()
		log.Info().Msg("Retry manager stopped")
	}
}

// Notify sends an event to all registered providers
func (rn *ResilientNotifier) Notify(ctx context.Context, event BaseEvent) {
	eventType := event.GetType()
	tenant := event.GetTenant()

	// Create a span for the notification
	ctx, span := rn.tracer.Start(ctx, "ResilientNotifier.Notify")
	defer span.End()

	span.SetAttributes(
		attribute.String("event.type", string(eventType)),
		attribute.String("tenant.organization", tenant.OrganizationID),
		attribute.String("tenant.space", tenant.SpaceID),
	)

	// Data lake storage for audit/history
	if rn.dataLake != nil {
		// A DataLake with retry mechanism could also be wrapped here
		if err := rn.dataLake.Store(ctx, event); err != nil {
			log.Error().
				Err(err).
				Str("event_type", string(eventType)).
				Msg("Failed to store event in data lake")
			span.RecordError(err)
		}
	}

	// Local function for processing individual providers
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

			// Add for retry attempts if retry is enabled
			if rn.resilienceConfig.RetryEnabled && rn.retryManager != nil {
				rn.retryManager.AddPendingEvent(event, providerName, 0)
			}
		} else {
			providerSpan.SetStatus(codes.Ok, "")
		}
	}

	// Processing based on Circuit Breaker configuration
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
		// Without Circuit Breaker
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

// GetCircuitBreakerStatus returns a status report about the Circuit Breaker
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

// GetRetryStatus returns a status report about the Retry queue
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

// ResetCircuitBreaker resets a Circuit Breaker for a specific provider
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

// ResetAllCircuitBreakers resets all Circuit Breakers
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
