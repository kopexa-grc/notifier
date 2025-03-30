// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

// BenchmarkProvider is a fast provider implementation for benchmark tests
type BenchmarkProvider struct {
	name       string
	mu         sync.Mutex
	eventCount int
	shouldFail bool
	failRate   float64
}

func NewBenchmarkProvider(name string) *BenchmarkProvider {
	return &BenchmarkProvider{
		name:       name,
		eventCount: 0,
		shouldFail: false,
		failRate:   0.0,
	}
}

func (p *BenchmarkProvider) WithFailRate(rate float64) *BenchmarkProvider {
	p.shouldFail = rate > 0
	p.failRate = rate
	return p
}

func (p *BenchmarkProvider) Send(ctx context.Context, event BaseEvent) error {
	p.mu.Lock()
	p.eventCount++
	currentCount := p.eventCount
	p.mu.Unlock()

	// Simulate occasional failures if configured
	if p.shouldFail && float64(currentCount%100)/100 < p.failRate {
		return fmt.Errorf("simulated error in provider %s", p.name)
	}

	return nil
}

func (p *BenchmarkProvider) Name() string {
	return p.name
}

func (p *BenchmarkProvider) GetEventCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.eventCount
}

// Benchmark setup functions
func setupBenchmarkNotifier(b *testing.B, batchSize int, eventsPerMinute int) *Notifier {
	// Disable logging for benchmark
	zerolog.SetGlobalLevel(zerolog.Disabled)

	config := NotifierConfig{
		MaxEventsPerMinute:  eventsPerMinute,
		BatchSize:           batchSize,
		BatchTimeoutSeconds: 1,
		RetentionDays:       30,
	}

	notifier := NewNotifier(config)
	provider := NewBenchmarkProvider("benchmark-provider")
	notifier.RegisterProvider(provider)

	// Start batch processing
	notifier.StartBatchProcessing()

	b.Cleanup(func() {
		notifier.StopBatchProcessing()
	})

	return notifier
}

func setupResilientBenchmarkNotifier(b *testing.B, failRate float64) *ResilientNotifier {
	// Disable logging for benchmark
	zerolog.SetGlobalLevel(zerolog.Disabled)

	config := NotifierConfig{
		MaxEventsPerMinute:  10000,
		BatchSize:           100,
		BatchTimeoutSeconds: 1,
		RetentionDays:       30,
	}

	resilienceConfig := ResilienceConfig{
		CircuitBreakerEnabled:     true,
		CircuitBreakerMaxFailures: 5,
		CircuitBreakerTimeout:     1 * time.Second,
		RetryEnabled:              true,
		MaxRetries:                2,
		RetryInitialDelay:         10 * time.Millisecond,
		RetryMaxDelay:             50 * time.Millisecond,
		RetryBackoffFactor:        1.5,
		PersistFailedEvents:       false,
	}

	// Pass nil for tracer and meter, they will be initialized in NewResilientNotifier
	notifier, err := NewResilientNotifier(config, resilienceConfig, nil, nil)
	if err != nil {
		b.Fatalf("Failed to create resilient notifier: %v", err)
		return nil
	}

	if notifier == nil {
		b.Fatal("Notifier is nil after creation")
		return nil
	}

	provider := NewBenchmarkProvider("benchmark-provider").WithFailRate(failRate)
	notifier.RegisterProvider(provider)
	notifier.Start()

	b.Cleanup(func() {
		notifier.Stop()
	})

	return notifier
}

func createTestBenchmarkEvent() BaseEvent {
	return &TestEvent{
		eventType: EventTypeInfo,
		tenant: TenantInfo{
			OrganizationID: "bench-org",
			SpaceID:        "bench-space",
		},
		payload:   "Benchmark-Payload",
		timestamp: time.Now(),
	}
}

// Benchmark for simple notifications
func BenchmarkSingleNotification(b *testing.B) {
	notifier := setupBenchmarkNotifier(b, 1, 100000)
	ctx := context.Background()
	event := createTestBenchmarkEvent()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		notifier.Notify(ctx, event)
	}
}

// Benchmark for batch processing
func BenchmarkBatchNotifications(b *testing.B) {
	batchSizes := []int{10, 50, 100, 200}

	for _, batchSize := range batchSizes {
		b.Run(fmt.Sprintf("BatchSize_%d", batchSize), func(b *testing.B) {
			notifier := setupBenchmarkNotifier(b, batchSize, 100000)
			// Batch processing is already started in setupBenchmarkNotifier

			ctx := context.Background()
			event := createTestBenchmarkEvent()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				notifier.Notify(ctx, event)
			}

			// Wait for batch processing to complete
			time.Sleep(100 * time.Millisecond)
		})
	}
}

// Benchmark for high load with concurrent events
func BenchmarkConcurrentNotifications(b *testing.B) {
	concurrencyLevels := []int{10, 50, 100}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(b *testing.B) {
			notifier := setupBenchmarkNotifier(b, 100, 100000)
			ctx := context.Background()

			// Create a worker pool
			var wg sync.WaitGroup

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for j := 0; j < concurrency; j++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						event := createTestBenchmarkEvent()
						notifier.Notify(ctx, event)
					}()
				}
				wg.Wait()
			}
		})
	}
}

// Benchmark for rate limiting
func BenchmarkRateLimiting(b *testing.B) {
	rateLimits := []int{100, 1000, 10000}

	for _, rateLimit := range rateLimits {
		b.Run(fmt.Sprintf("RateLimit_%d", rateLimit), func(b *testing.B) {
			notifier := setupBenchmarkNotifier(b, 10, rateLimit)
			ctx := context.Background()
			event := createTestBenchmarkEvent()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				notifier.Notify(ctx, event)
			}
		})
	}
}

// Benchmark for resilience mechanisms
func BenchmarkResilienceMechanisms(b *testing.B) {
	failRates := []float64{0.0, 0.1, 0.3}

	for _, failRate := range failRates {
		b.Run(fmt.Sprintf("FailRate_%.1f", failRate), func(b *testing.B) {
			notifier := setupResilientBenchmarkNotifier(b, failRate)
			ctx := context.Background()
			event := createTestBenchmarkEvent()

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				notifier.Notify(ctx, event)
			}

			// Short wait to allow retry attempts to complete
			time.Sleep(100 * time.Millisecond)
		})
	}
}
