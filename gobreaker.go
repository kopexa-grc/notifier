// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/sony/gobreaker/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// GoBreakerWrapper wraps the sony/gobreaker CircuitBreaker implementation
// to provide a compatible interface with our existing circuit breaker usage.
type GoBreakerWrapper struct {
	name        string
	cb          *gobreaker.CircuitBreaker[interface{}]
	maxFailures int
	timeout     time.Duration
	meter       metric.Meter
	stateGauge  metric.Int64ObservableGauge
	tracer      trace.Tracer
}

// NewGoBreakerWrapper creates a new CircuitBreaker instance using sony/gobreaker
func NewGoBreakerWrapper(name string, maxFailures int, timeout time.Duration, meter metric.Meter) *GoBreakerWrapper {
	wrapper := &GoBreakerWrapper{
		name:        name,
		maxFailures: maxFailures,
		timeout:     timeout,
		meter:       meter,
	}

	// Create the circuit breaker with the generic type interface{}
	wrapper.cb = gobreaker.NewCircuitBreaker[interface{}](wrapper.createSettings())

	// Initialize observability metrics if meter is provided
	if meter != nil {
		// Create gauge for observing circuit state (0=Closed, 1=Open, 2=HalfOpen)
		stateGauge, err := meter.Int64ObservableGauge(
			"notifier.circuit_breaker.state",
			metric.WithDescription("Current state of the circuit breaker (0=Closed, 1=Open, 2=HalfOpen)"),
		)
		if err == nil {
			wrapper.stateGauge = stateGauge
			// Register callback function for asynchronous gauge updates
			_, err := meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
				stateVal := int64(0)
				switch wrapper.cb.State() {
				case gobreaker.StateClosed:
					stateVal = 0
				case gobreaker.StateOpen:
					stateVal = 1
				case gobreaker.StateHalfOpen:
					stateVal = 2
				}

				o.ObserveInt64(stateGauge, stateVal, metric.WithAttributes(
					attribute.String("provider", wrapper.name),
				))
				return nil
			}, stateGauge)
			if err != nil {
				log.Error().Err(err).Str("provider", name).Msg(LogFailedToRegisterMetric)
			}
		}
	}

	log.Info().
		Str("provider", name).
		Int("max_failures", maxFailures).
		Str("timeout", timeout.String()).
		Msg(LogCircuitBreakerCreated)

	return wrapper
}

// createSettings creates the settings for the circuit breaker
func (w *GoBreakerWrapper) createSettings() gobreaker.Settings {
	return gobreaker.Settings{
		Name:        w.name,
		MaxRequests: 1, // Default: allow only 1 request in half-open state
		Interval:    0, // Don't clear counts automatically
		Timeout:     w.timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= uint32(w.maxFailures)
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			switch to {
			case gobreaker.StateClosed:
				log.Info().
					Str("provider", name).
					Msg(LogCircuitBreakerClosed)
			case gobreaker.StateOpen:
				log.Warn().
					Str("provider", name).
					Msg(LogCircuitBreakerTripped)
			case gobreaker.StateHalfOpen:
				log.Info().
					Str("provider", name).
					Msg(LogCircuitBreakerHalfOpen)
			}
		},
	}
}

// OnSuccess tracks successful operations
func (w *GoBreakerWrapper) OnSuccess() {
	// gobreaker handles success tracking internally when Execute or Allow is called
	// This method is provided for compatibility with the existing API
}

// OnFailure tracks failed operations
func (w *GoBreakerWrapper) OnFailure() {
	// gobreaker handles failure tracking internally when Execute or Allow is called
	// This method is provided for compatibility with the existing API
}

// IsOpen checks if the circuit is open
func (w *GoBreakerWrapper) IsOpen() bool {
	return w.cb.State() == gobreaker.StateOpen
}

// Reset forces the circuit breaker back to the closed state
func (w *GoBreakerWrapper) Reset() {
	// The gobreaker library doesn't have a direct Reset method in v2
	// We need to create a new circuit breaker with the same settings
	w.cb = gobreaker.NewCircuitBreaker[interface{}](w.createSettings())

	log.Info().
		Str("provider", w.name).
		Msg(LogCircuitBreakerReset)
}

// GetState returns the current state of the circuit breaker
func (w *GoBreakerWrapper) GetState() CircuitBreakerState {
	switch w.cb.State() {
	case gobreaker.StateClosed:
		return CircuitClosed
	case gobreaker.StateOpen:
		return CircuitOpen
	case gobreaker.StateHalfOpen:
		return CircuitHalfOpen
	default:
		// This shouldn't happen with gobreaker, but for safety
		return CircuitClosed
	}
}

// ProviderWithGoBreakerWrapper wraps a provider with a GoBreaker Circuit Breaker
type ProviderWithGoBreakerWrapper struct {
	provider       Provider
	circuitBreaker *GoBreakerWrapper
	tracer         trace.Tracer
}

// NewProviderWithGoBreakerWrapper creates a new Provider with Circuit Breaker
func NewProviderWithGoBreakerWrapper(provider Provider, cb *GoBreakerWrapper, tracer trace.Tracer) *ProviderWithGoBreakerWrapper {
	return &ProviderWithGoBreakerWrapper{
		provider:       provider,
		circuitBreaker: cb,
		tracer:         tracer,
	}
}

// Send sends an event to the provider, respecting the circuit breaker state
func (p *ProviderWithGoBreakerWrapper) Send(ctx context.Context, event BaseEvent) error {
	// Use gobreaker's Execute to handle circuit breaker logic
	result, err := p.circuitBreaker.cb.Execute(func() (interface{}, error) {
		var span trace.Span
		if p.tracer != nil {
			ctx, span = p.tracer.Start(ctx, "ProviderWithGoBreakerWrapper.Send")
			span.SetAttributes(
				attribute.String("provider", p.provider.Name()),
				attribute.String("event_type", string(event.GetType())),
				attribute.String("circuit_state", p.circuitBreaker.GetState().String()),
			)
			defer span.End()
		}

		// Call the underlying provider
		err := p.provider.Send(ctx, event)
		return nil, err // gobreaker expects this format
	})

	if err != nil {
		if err == gobreaker.ErrOpenState {
			return fmt.Errorf("circuit breaker is open for provider %s", p.provider.Name())
		}
		return err
	}

	// Result will be nil on success, based on our Execute implementation
	_ = result
	return nil
}

// Name returns the name of the provider
func (p *ProviderWithGoBreakerWrapper) Name() string {
	return p.provider.Name()
}

// GetState returns the current state of the circuit breaker
func (p *ProviderWithGoBreakerWrapper) GetState() CircuitBreakerState {
	return p.circuitBreaker.GetState()
}

// ResetCircuitBreaker resets the Circuit Breaker
func (p *ProviderWithGoBreakerWrapper) ResetCircuitBreaker() {
	p.circuitBreaker.Reset()
}
