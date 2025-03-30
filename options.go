// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

// Options pattern for configuration
// New(..Options) *Notifier

type Option func(*options)

type options struct {
	// List of providers
	providers []Provider
	datalake  DataLake

	maxEventsPerMinute  int
	batchSize           int
	batchTimeoutSeconds int
	retentionDays       int

	circuitBreakerEnabled     bool
	circuitBreakerMaxFailures int
	circuitBreakerTimeoutSec  int
	retryEnabled              bool
	maxRetries                int
	retryInitialDelaySec      int
	retryMaxDelaySec          int
	retryBackoffFactor        float64
	persistFailedEvents       bool
}

func defaultOptions() *options {
	return &options{
		maxEventsPerMinute:        DefaultMaxEventsPerMinute,
		batchSize:                 DefaultBatchSize,
		batchTimeoutSeconds:       DefaultBatchTimeoutSeconds,
		retentionDays:             DefaultRetentionDays,
		circuitBreakerEnabled:     false,
		circuitBreakerMaxFailures: DefaultCircuitBreakerMaxFailures,
		circuitBreakerTimeoutSec:  DefaultCircuitBreakerTimeoutSec,
		retryEnabled:              false,
		maxRetries:                DefaultMaxRetries,
		retryInitialDelaySec:      DefaultRetryInitialDelaySec,
		retryMaxDelaySec:          DefaultRetryMaxDelaySec,
		retryBackoffFactor:        DefaultRetryBackoffFactor,
		persistFailedEvents:       false,
	}
}

// WithProvider adds a provider to the notifier
func WithProvider(provider Provider) Option {
	return func(o *options) {
		if o.providers == nil {
			o.providers = []Provider{}
		}
		o.providers = append(o.providers, provider)
	}
}

// WithDatalake adds a datalake to the notifier
func WithDatalake(datalake DataLake) Option {
	return func(o *options) {
		o.datalake = datalake
	}
}

// WithMaxEventsPerMinute sets the maximum number of events per minute
func WithMaxEventsPerMinute(maxEventsPerMinute int) Option {
	return func(o *options) {
		o.maxEventsPerMinute = maxEventsPerMinute
	}
}

// WithBatchSize sets the batch size
func WithBatchSize(batchSize int) Option {
	return func(o *options) {
		o.batchSize = batchSize
	}
}

// WithBatchTimeoutSeconds sets the batch timeout
func WithBatchTimeoutSeconds(batchTimeoutSeconds int) Option {
	return func(o *options) {
		o.batchTimeoutSeconds = batchTimeoutSeconds
	}
}

// WithRetentionDays sets the retention days
func WithRetentionDays(retentionDays int) Option {
	return func(o *options) {
		o.retentionDays = retentionDays
	}
}

// WithCircuitBreakerEnabled sets the circuit breaker enabled
func WithCircuitBreakerEnabled(circuitBreakerEnabled bool) Option {
	return func(o *options) {
		o.circuitBreakerEnabled = circuitBreakerEnabled
	}
}

// WithCircuitBreakerMaxFailures sets the circuit breaker max failures
func WithCircuitBreakerMaxFailures(circuitBreakerMaxFailures int) Option {
	return func(o *options) {
		o.circuitBreakerMaxFailures = circuitBreakerMaxFailures
	}
}

// WithCircuitBreakerTimeoutSec sets the circuit breaker timeout
func WithCircuitBreakerTimeoutSec(circuitBreakerTimeoutSec int) Option {
	return func(o *options) {
		o.circuitBreakerTimeoutSec = circuitBreakerTimeoutSec
	}
}

// WithRetryEnabled sets the retry enabled
func WithRetryEnabled(retryEnabled bool) Option {
	return func(o *options) {
		o.retryEnabled = retryEnabled
	}
}

// WithMaxRetries sets the max retries
func WithMaxRetries(maxRetries int) Option {
	return func(o *options) {
		o.maxRetries = maxRetries
	}
}

// WithRetryInitialDelaySec sets the retry initial delay
func WithRetryInitialDelaySec(retryInitialDelaySec int) Option {
	return func(o *options) {
		o.retryInitialDelaySec = retryInitialDelaySec
	}
}

// WithRetryMaxDelaySec sets the retry max delay
func WithRetryMaxDelaySec(retryMaxDelaySec int) Option {
	return func(o *options) {
		o.retryMaxDelaySec = retryMaxDelaySec
	}
}

// WithRetryBackoffFactor sets the retry backoff factor
func WithRetryBackoffFactor(retryBackoffFactor float64) Option {
	return func(o *options) {
		o.retryBackoffFactor = retryBackoffFactor
	}
}

// WithPersistFailedEvents sets the persist failed events
func WithPersistFailedEvents(persistFailedEvents bool) Option {
	return func(o *options) {
		o.persistFailedEvents = persistFailedEvents
	}
}
