// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"fmt"
	"time"
)

// ValidationError represents an error during configuration validation
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("invalid configuration for %s: %s", e.Field, e.Message)
}

// ConfigValidator provides methods to validate service configurations
type ConfigValidator struct {
	// Optional minimum and maximum values for various parameters
	minEventsPerMinute int
	maxEventsPerMinute int
	minBatchSize       int
	maxBatchSize       int
	minRetentionDays   int
	maxRetentionDays   int
	minMaxRetries      int
	maxMaxRetries      int
	minCircuitFailures int
	maxCircuitFailures int
	minCircuitTimeout  time.Duration
	maxCircuitTimeout  time.Duration
	minRetryDelay      time.Duration
	maxRetryDelay      time.Duration
	minBackoffFactor   float64
	maxBackoffFactor   float64
}

// NewConfigValidator creates a new validator with default parameter bounds
func NewConfigValidator() *ConfigValidator {
	return &ConfigValidator{
		minEventsPerMinute: 1,
		maxEventsPerMinute: 10000,
		minBatchSize:       1,
		maxBatchSize:       1000,
		minRetentionDays:   1,
		maxRetentionDays:   3650, // 10 years
		minMaxRetries:      0,
		maxMaxRetries:      100,
		minCircuitFailures: 1,
		maxCircuitFailures: 100,
		minCircuitTimeout:  time.Second,
		maxCircuitTimeout:  time.Hour * 24,
		minRetryDelay:      time.Millisecond * 10,
		maxRetryDelay:      time.Hour,
		minBackoffFactor:   1.0,
		maxBackoffFactor:   10.0,
	}
}

// WithEventRateLimits sets custom min/max values for events per minute
func (v *ConfigValidator) WithEventRateLimits(min, max int) *ConfigValidator {
	v.minEventsPerMinute = min
	v.maxEventsPerMinute = max
	return v
}

// WithBatchSizeLimits sets custom min/max values for batch size
func (v *ConfigValidator) WithBatchSizeLimits(min, max int) *ConfigValidator {
	v.minBatchSize = min
	v.maxBatchSize = max
	return v
}

// WithRetentionDaysLimits sets custom min/max values for retention days
func (v *ConfigValidator) WithRetentionDaysLimits(min, max int) *ConfigValidator {
	v.minRetentionDays = min
	v.maxRetentionDays = max
	return v
}

// ValidateNotifierConfig validates the notifier configuration
func (v *ConfigValidator) ValidateNotifierConfig(config NotifierConfig) []ValidationError {
	var errors []ValidationError

	// Check MaxEventsPerMinute
	if config.MaxEventsPerMinute < v.minEventsPerMinute {
		errors = append(errors, ValidationError{
			Field:   "MaxEventsPerMinute",
			Message: fmt.Sprintf("must be at least %d", v.minEventsPerMinute),
		})
	} else if config.MaxEventsPerMinute > v.maxEventsPerMinute {
		errors = append(errors, ValidationError{
			Field:   "MaxEventsPerMinute",
			Message: fmt.Sprintf("cannot exceed %d", v.maxEventsPerMinute),
		})
	}

	// Check BatchSize
	if config.BatchSize < v.minBatchSize {
		errors = append(errors, ValidationError{
			Field:   "BatchSize",
			Message: fmt.Sprintf("must be at least %d", v.minBatchSize),
		})
	} else if config.BatchSize > v.maxBatchSize {
		errors = append(errors, ValidationError{
			Field:   "BatchSize",
			Message: fmt.Sprintf("cannot exceed %d", v.maxBatchSize),
		})
	}

	// Check BatchTimeoutSeconds
	if config.BatchTimeoutSeconds <= 0 {
		errors = append(errors, ValidationError{
			Field:   "BatchTimeoutSeconds",
			Message: "must be greater than 0",
		})
	} else if config.BatchTimeoutSeconds > 300 { // 5 minutes is reasonable max
		errors = append(errors, ValidationError{
			Field:   "BatchTimeoutSeconds",
			Message: "cannot exceed 300 seconds (5 minutes)",
		})
	}

	// Check RetentionDays
	if config.RetentionDays < v.minRetentionDays {
		errors = append(errors, ValidationError{
			Field:   "RetentionDays",
			Message: fmt.Sprintf("must be at least %d", v.minRetentionDays),
		})
	} else if config.RetentionDays > v.maxRetentionDays {
		errors = append(errors, ValidationError{
			Field:   "RetentionDays",
			Message: fmt.Sprintf("cannot exceed %d", v.maxRetentionDays),
		})
	}

	return errors
}

// ValidateResilienceConfig validates the resilience configuration
func (v *ConfigValidator) ValidateResilienceConfig(config ResilienceConfig) []ValidationError {
	var errors []ValidationError

	// Only validate circuit breaker settings if enabled
	if config.CircuitBreakerEnabled {
		// Check CircuitBreakerMaxFailures
		if config.CircuitBreakerMaxFailures < v.minCircuitFailures {
			errors = append(errors, ValidationError{
				Field:   "CircuitBreakerMaxFailures",
				Message: fmt.Sprintf("must be at least %d", v.minCircuitFailures),
			})
		} else if config.CircuitBreakerMaxFailures > v.maxCircuitFailures {
			errors = append(errors, ValidationError{
				Field:   "CircuitBreakerMaxFailures",
				Message: fmt.Sprintf("cannot exceed %d", v.maxCircuitFailures),
			})
		}

		// Check CircuitBreakerTimeout
		if config.CircuitBreakerTimeout < v.minCircuitTimeout {
			errors = append(errors, ValidationError{
				Field:   "CircuitBreakerTimeout",
				Message: fmt.Sprintf("must be at least %s", v.minCircuitTimeout),
			})
		} else if config.CircuitBreakerTimeout > v.maxCircuitTimeout {
			errors = append(errors, ValidationError{
				Field:   "CircuitBreakerTimeout",
				Message: fmt.Sprintf("cannot exceed %s", v.maxCircuitTimeout),
			})
		}
	}

	// Only validate retry settings if enabled
	if config.RetryEnabled {
		// Check MaxRetries
		if config.MaxRetries < v.minMaxRetries {
			errors = append(errors, ValidationError{
				Field:   "MaxRetries",
				Message: fmt.Sprintf("must be at least %d", v.minMaxRetries),
			})
		} else if config.MaxRetries > v.maxMaxRetries {
			errors = append(errors, ValidationError{
				Field:   "MaxRetries",
				Message: fmt.Sprintf("cannot exceed %d", v.maxMaxRetries),
			})
		}

		// Check RetryInitialDelay
		if config.RetryInitialDelay < v.minRetryDelay {
			errors = append(errors, ValidationError{
				Field:   "RetryInitialDelay",
				Message: fmt.Sprintf("must be at least %s", v.minRetryDelay),
			})
		} else if config.RetryInitialDelay > v.maxRetryDelay {
			errors = append(errors, ValidationError{
				Field:   "RetryInitialDelay",
				Message: fmt.Sprintf("cannot exceed %s", v.maxRetryDelay),
			})
		}

		// Check RetryMaxDelay
		if config.RetryMaxDelay < config.RetryInitialDelay {
			errors = append(errors, ValidationError{
				Field:   "RetryMaxDelay",
				Message: "must be greater than or equal to RetryInitialDelay",
			})
		} else if config.RetryMaxDelay > v.maxRetryDelay {
			errors = append(errors, ValidationError{
				Field:   "RetryMaxDelay",
				Message: fmt.Sprintf("cannot exceed %s", v.maxRetryDelay),
			})
		}

		// Check RetryBackoffFactor
		if config.RetryBackoffFactor < v.minBackoffFactor {
			errors = append(errors, ValidationError{
				Field:   "RetryBackoffFactor",
				Message: fmt.Sprintf("must be at least %.1f", v.minBackoffFactor),
			})
		} else if config.RetryBackoffFactor > v.maxBackoffFactor {
			errors = append(errors, ValidationError{
				Field:   "RetryBackoffFactor",
				Message: fmt.Sprintf("cannot exceed %.1f", v.maxBackoffFactor),
			})
		}
	}

	return errors
}

// ValidateConfigs validates both notifier and resilience configurations together
func (v *ConfigValidator) ValidateConfigs(notifierConfig NotifierConfig, resilienceConfig ResilienceConfig) []ValidationError {
	errors := v.ValidateNotifierConfig(notifierConfig)
	errors = append(errors, v.ValidateResilienceConfig(resilienceConfig)...)
	return errors
}
