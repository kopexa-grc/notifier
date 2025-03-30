// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"
)

// Provider defines the interface for all
// notification providers
type Provider interface {
	// Send sends a notification
	Send(ctx context.Context, event BaseEvent) error

	// Name returns the unique name of the provider
	Name() string
}

// NotifierConfig contains configuration options for enterprise-grade features
type NotifierConfig struct {
	// Rate limiting
	MaxEventsPerMinute int
	// Batching
	BatchSize           int
	BatchTimeoutSeconds int
	// Data retention
	RetentionDays int
	// OpenTelemetry
	TracerName         string
	MeterName          string
	PropagateContext   bool
	EnabledDetailTrace bool
}

// EventStats tracks analytics data for event types
type EventStats struct {
	TotalCount    int
	SuccessCount  int
	FailureCount  int
	LastProcessed time.Time
	AverageTimeMs int64
}

// batchItem represents an item to be processed in a batch
type batchItem struct {
	ctx   context.Context
	event BaseEvent
}

// Notifier is the central service for managing and
// distributing notifications with enterprise-grade capabilities
type Notifier struct {
	providers []Provider
	dataLake  DataLake
	mu        sync.RWMutex

	// Configuration
	config NotifierConfig

	// Rate limiting
	rateLimiters     map[string]*rate.Limiter
	rateLimiterMutex sync.RWMutex

	// OpenTelemetry
	tracer trace.Tracer
	meter  metric.Meter

	// OTel Metrics
	eventCounter           metric.Int64Counter
	eventDuration          metric.Float64Histogram
	providerCounter        metric.Int64Counter
	providerDuration       metric.Float64Histogram
	dlOperationCounter     metric.Int64Counter
	rateLimitCounter       metric.Int64Counter
	batchProcessingCounter metric.Int64Counter

	// Batching support
	batching struct {
		enabled        bool
		size           int
		timeoutSeconds int
		workers        int
		batchChan      chan *batchItem
		stopChan       chan struct{}
		wg             sync.WaitGroup
	}

	// For gathering stats without OpenTelemetry
	stats struct {
		mu          sync.RWMutex
		eventStats  map[EventType]*EventStats
		lastUpdated time.Time
	}
}

// NewNotifier creates a new instance of the Notifier with enterprise configuration
func NewNotifier(config NotifierConfig, providers ...Provider) *Notifier {
	// Provide defaults for unspecified values
	if config.MaxEventsPerMinute <= 0 {
		config.MaxEventsPerMinute = 100 // Default ~100 per minute
	}

	if config.BatchSize <= 0 {
		config.BatchSize = 10
	}

	if config.BatchTimeoutSeconds <= 0 {
		config.BatchTimeoutSeconds = 30
	}

	if config.RetentionDays <= 0 {
		config.RetentionDays = 90 // Default: 3 months
	}

	if config.TracerName == "" {
		config.TracerName = "github.com/kopexa-grc/kopexa/notifier"
	}

	if config.MeterName == "" {
		config.MeterName = "github.com/kopexa-grc/kopexa/notifier"
	}

	// Create the base notifier
	n := &Notifier{
		providers:    providers,
		config:       config,
		rateLimiters: make(map[string]*rate.Limiter),
	}

	// Initialize OpenTelemetry
	n.tracer = otel.Tracer(config.TracerName)
	n.meter = otel.Meter(config.MeterName)

	// Setup metrics
	n.initializeMetrics()

	// Initialize local stats tracking as fallback
	n.stats.eventStats = make(map[EventType]*EventStats)
	n.stats.lastUpdated = time.Now()

	// Initialize batching system
	n.batching.size = config.BatchSize
	n.batching.timeoutSeconds = config.BatchTimeoutSeconds
	n.batching.workers = DefaultWorkerCount

	// If batching is enabled, initialize channels and start workers
	if config.BatchSize > 1 {
		n.EnableBatching(true)
	}

	// Initialize rate limiters
	n.initializeRateLimiting()

	log.Info().
		Int("max_events_per_minute", config.MaxEventsPerMinute).
		Int("batch_size", config.BatchSize).
		Int("batch_timeout_sec", config.BatchTimeoutSeconds).
		Int("retention_days", config.RetentionDays).
		Msg(LogInitNotifier)

	return n
}

// initializeRateLimiting sets up rate limiters for all tenants
func (n *Notifier) initializeRateLimiting() {
	// Initialize global rate limiter
	ratePerSecond := float64(n.config.MaxEventsPerMinute) / 60.0
	burstSize := DefaultBucketSize

	// Use standard rate limiter from golang.org/x/time/rate
	n.rateLimiters["global"] = rate.NewLimiter(rate.Limit(ratePerSecond), burstSize)
}

// isRateLimited checks if an event should be rate limited
func (n *Notifier) isRateLimited(event BaseEvent) bool {
	n.rateLimiterMutex.RLock()
	defer n.rateLimiterMutex.RUnlock()

	tenant := event.GetTenant()
	tenantKey := tenant.OrganizationID
	if tenantKey == "" {
		tenantKey = "global"
	}

	// Check if we have a tenant-specific rate limiter
	limiter, ok := n.rateLimiters[tenantKey]
	if !ok {
		// Try global rate limiter
		limiter, ok = n.rateLimiters["global"]
		if !ok {
			// No rate limiter, allow the event
			return false
		}
	}

	allowed := limiter.Allow()

	// Record rate limiting metrics
	if !allowed && n.rateLimitCounter != nil {
		ctx := context.Background()
		n.rateLimitCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("organization_id", tenant.OrganizationID),
			attribute.String("event_type", string(event.GetType())),
		))
	}

	return !allowed
}

// SetMaxEventsPerMinute updates the rate limit
func (n *Notifier) SetMaxEventsPerMinute(maxEvents int) {
	n.rateLimiterMutex.Lock()
	defer n.rateLimiterMutex.Unlock()

	n.config.MaxEventsPerMinute = maxEvents
	ratePerSecond := float64(maxEvents) / 60.0

	// Update all rate limiters
	for tenant, limiter := range n.rateLimiters {
		burstSize := limiter.Burst()
		// Create a new limiter with updated rate
		n.rateLimiters[tenant] = rate.NewLimiter(rate.Limit(ratePerSecond), burstSize)
	}

	log.Info().Int("max_events_per_minute", maxEvents).Msg(LogRateLimitUpdated)
}

// SetRetentionDays updates the retention period for notifications
func (n *Notifier) SetRetentionDays(days int) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if days <= 0 {
		days = DefaultRetentionDays
	}

	n.config.RetentionDays = days
	log.Info().Int("retention_days", days).Msg(LogRetentionPolicyUpdated)
}

// initializeMetrics sets up all the OpenTelemetry metrics
func (n *Notifier) initializeMetrics() {
	var err error

	// Event processing metrics
	n.eventCounter, err = n.meter.Int64Counter(
		"notifier.events.total",
		metric.WithDescription("Total number of events processed by the notifier"),
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create event counter metric")
	}

	n.eventDuration, err = n.meter.Float64Histogram(
		"notifier.events.duration",
		metric.WithDescription("Duration of event processing in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create event duration metric")
	}

	// Provider metrics
	n.providerCounter, err = n.meter.Int64Counter(
		"notifier.provider.operations",
		metric.WithDescription("Total number of send operations by provider"),
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create provider counter metric")
	}

	n.providerDuration, err = n.meter.Float64Histogram(
		"notifier.provider.duration",
		metric.WithDescription("Duration of provider send operations in milliseconds"),
		metric.WithUnit("ms"),
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create provider duration metric")
	}

	// Data lake metrics
	n.dlOperationCounter, err = n.meter.Int64Counter(
		"notifier.datalake.operations",
		metric.WithDescription("Total number of data lake operations"),
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create data lake counter metric")
	}

	// Rate limiting metrics
	n.rateLimitCounter, err = n.meter.Int64Counter(
		"notifier.ratelimit.limited",
		metric.WithDescription("Number of rate limited events"),
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create rate limit counter metric")
	}

	// Batch processing metrics
	n.batchProcessingCounter, err = n.meter.Int64Counter(
		"notifier.batch.processed",
		metric.WithDescription("Number of batches processed"),
	)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to create batch counter metric")
	}
}

// now returns the current time, extracted into a method
// to make testing easier and time handling consistent
func (n *Notifier) now() time.Time {
	return time.Now()
}

// RegisterProvider registers a new notification provider
func (n *Notifier) RegisterProvider(provider Provider) {
	n.mu.Lock()
	defer n.mu.Unlock()

	log.Info().Str("provider", provider.Name()).Msg("Registering notification provider")
	n.providers = append(n.providers, provider)
}

// SetDataLake assigns a data lake implementation to the notifier
func (n *Notifier) SetDataLake(dataLake DataLake) {
	n.dataLake = dataLake
	log.Info().Str("dataLake", dataLake.Name()).Msg("Data lake storage enabled for notifications")
}

// GetEventStats returns analytics data about event processing
func (n *Notifier) GetEventStats() map[EventType]*EventStats {
	// First try to get current stats from OpenTelemetry metrics
	// If that fails, fall back to local stats

	n.stats.mu.RLock()
	defer n.stats.mu.RUnlock()

	// Copy the stats to prevent modification during use
	result := make(map[EventType]*EventStats, len(n.stats.eventStats))
	for k, v := range n.stats.eventStats {
		statsCopy := *v
		result[k] = &statsCopy
	}

	return result
}

// updateEventStats updates the local stats tracking for an event
func (n *Notifier) updateEventStats(eventType EventType, success bool, duration time.Duration) {
	n.stats.mu.Lock()
	defer n.stats.mu.Unlock()

	if n.stats.eventStats[eventType] == nil {
		n.stats.eventStats[eventType] = &EventStats{
			LastProcessed: time.Now(),
		}
	}

	stats := n.stats.eventStats[eventType]
	stats.TotalCount++
	if success {
		stats.SuccessCount++
	} else {
		stats.FailureCount++
	}

	// Update average processing time
	currentTotal := stats.AverageTimeMs * int64(stats.TotalCount-1)
	newTotal := currentTotal + duration.Milliseconds()
	stats.AverageTimeMs = newTotal / int64(stats.TotalCount)

	stats.LastProcessed = time.Now()
	n.stats.lastUpdated = time.Now()
}

// Notify processes an event and sends notifications via
// registered providers with enterprise-grade features
func (n *Notifier) Notify(ctx context.Context, event BaseEvent) {
	// Start a new span for tracing
	ctx, span := n.tracer.Start(ctx,
		"Notifier.Notify",
		trace.WithAttributes(
			attribute.String("event.type", string(event.GetType())),
		),
	)
	defer span.End()

	startTime := time.Now()
	tenant := event.GetTenant()
	eventType := event.GetType()

	// Record event received in metrics
	n.eventCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("event_type", string(eventType)),
		attribute.String("status", "received"),
	))

	// Apply rate limiting if enabled
	if n.isRateLimited(event) {
		log.Warn().
			Str("org_id", tenant.OrganizationID).
			Str("event_type", string(eventType)).
			Msg("Event dropped due to rate limiting")

		n.rateLimitCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("org_id", tenant.OrganizationID),
			attribute.String("event_type", string(eventType)),
		))

		n.eventCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("event_type", string(eventType)),
			attribute.String("status", "rate_limited"),
		))

		span.SetStatus(codes.Error, "Rate limited")
		return
	}

	// Store in data lake if configured
	if n.dataLake != nil {
		dlCtx, dlSpan := n.tracer.Start(ctx, "DataLake.Store")
		err := n.dataLake.Store(dlCtx, event)
		if err != nil {
			log.Error().
				Err(err).
				Str("event_type", string(eventType)).
				Msg("Failed to store event in data lake")

			n.dlOperationCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "store"),
				attribute.String("status", "error"),
				attribute.String("error", err.Error()),
			))

			dlSpan.RecordError(err)
			dlSpan.SetStatus(codes.Error, "Failed to store in DataLake")
		} else {
			n.dlOperationCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("operation", "store"),
				attribute.String("status", "success"),
			))
		}
		dlSpan.End()
	}

	// If batching is enabled, add to batch queue instead of processing immediately
	if n.batching.enabled {
		select {
		case n.batching.batchChan <- &batchItem{ctx: ctx, event: event}:
			// Successfully added to batch queue
			span.AddEvent("Added to batch queue")
		default:
			// Queue is full, process immediately
			log.Warn().
				Str("event_type", string(eventType)).
				Msg("Batch queue full, processing event immediately")

			span.AddEvent("Batch queue full, processing immediately")
			n.processEvent(ctx, event, startTime)
		}
		return
	}

	// Process event immediately if batching is disabled
	n.processEvent(ctx, event, startTime)
}

// processEvent handles the actual event processing
func (n *Notifier) processEvent(ctx context.Context, event BaseEvent, startTime time.Time) {
	ctx, span := n.tracer.Start(ctx, "Notifier.processEvent")
	defer span.End()

	eventType := event.GetType()

	// Asynchronous processing in a goroutine
	go func() {
		// Create a new context with a new span for the async work
		asyncCtx, asyncSpan := n.tracer.Start(context.Background(), "Notifier.processEvent.async")
		defer asyncSpan.End()

		// Copy trace context from original request if configured
		if n.config.PropagateContext {
			// This would involve propagating trace context from ctx to asyncCtx
			// Using otel context propagation
		}

		n.mu.RLock()
		providers := make([]Provider, len(n.providers))
		copy(providers, n.providers) // Create a copy to avoid holding the lock
		n.mu.RUnlock()

		log.Debug().
			Str("event_type", string(eventType)).
			Int("providers", len(providers)).
			Msg("Processing notification event")

		asyncSpan.SetAttributes(
			attribute.Int("provider_count", len(providers)),
			attribute.String("event_type", string(eventType)),
		)

		// Track success status
		success := true
		var wg sync.WaitGroup

		// Iterate through all providers and send notifications
		for _, provider := range providers {
			wg.Add(1)
			providerName := provider.Name()

			// Start each provider in its own goroutine
			go func(p Provider) {
				defer wg.Done()

				// Create provider span
				providerCtx, providerSpan := n.tracer.Start(asyncCtx,
					fmt.Sprintf("Provider.%s.Send", p.Name()),
					trace.WithAttributes(
						attribute.String("provider.name", p.Name()),
						attribute.String("event.type", string(eventType)),
					),
				)
				defer providerSpan.End()

				// Measure provider latency
				providerStart := time.Now()
				err := p.Send(providerCtx, event)
				elapsed := time.Since(providerStart)

				n.providerDuration.Record(asyncCtx, float64(elapsed.Milliseconds()), metric.WithAttributes(
					attribute.String("provider", providerName),
				))

				if err != nil {
					success = false
					log.Error().
						Err(err).
						Str("provider", providerName).
						Str("event_type", string(eventType)).
						Msg("Failed to send notification")

					n.providerCounter.Add(asyncCtx, 1, metric.WithAttributes(
						attribute.String("provider", providerName),
						attribute.String("status", "error"),
						attribute.String("error", err.Error()),
					))

					providerSpan.RecordError(err)
					providerSpan.SetStatus(codes.Error, "Provider send failed")
				} else {
					n.providerCounter.Add(asyncCtx, 1, metric.WithAttributes(
						attribute.String("provider", providerName),
						attribute.String("status", "success"),
					))

					providerSpan.SetStatus(codes.Ok, "")
				}
			}(provider)
		}

		// Wait until all providers are finished
		wg.Wait()

		// Calculate and record end-to-end latency
		processingTime := time.Since(startTime)

		n.eventDuration.Record(asyncCtx, float64(processingTime.Milliseconds()), metric.WithAttributes(
			attribute.String("event_type", string(eventType)),
		))

		// Record success/failure
		if success {
			n.eventCounter.Add(asyncCtx, 1, metric.WithAttributes(
				attribute.String("event_type", string(eventType)),
				attribute.String("status", "success"),
			))
			asyncSpan.SetStatus(codes.Ok, "")
		} else {
			n.eventCounter.Add(asyncCtx, 1, metric.WithAttributes(
				attribute.String("event_type", string(eventType)),
				attribute.String("status", "error"),
			))
			asyncSpan.SetStatus(codes.Error, "One or more providers failed")
		}

		// Update local stats tracking
		n.updateEventStats(eventType, success, processingTime)

		log.Debug().
			Str("event_type", string(eventType)).
			Bool("success", success).
			Float64("latency_ms", float64(processingTime.Milliseconds())).
			Msg("Notification processing completed")
	}()
}

// StartBatchProcessing starts the batch processing workers
func (n *Notifier) StartBatchProcessing() {
	if !n.batching.enabled {
		return
	}

	log.Info().
		Int("workers", n.batching.workers).
		Int("batch_size", n.batching.size).
		Int("timeout_seconds", n.batching.timeoutSeconds).
		Msg("Starting batch processing workers")

	// Start worker goroutines
	for i := 0; i < n.batching.workers; i++ {
		n.batching.wg.Add(1)
		go n.batchWorker(i)
	}
}

// StopBatchProcessing stops all batch processing workers
func (n *Notifier) StopBatchProcessing() {
	if !n.batching.enabled {
		return
	}

	log.Info().Msg("Stopping batch processing workers")
	close(n.batching.stopChan)

	// Wait for all workers to finish
	done := make(chan struct{})
	go func() {
		n.batching.wg.Wait()
		close(done)
	}()

	// Wait with timeout
	select {
	case <-done:
		log.Info().Msg("All batch workers stopped gracefully")
	case <-time.After(10 * time.Second):
		log.Warn().Msg("Timeout waiting for batch workers to stop")
	}
}

// batchWorker processes batches of events
func (n *Notifier) batchWorker(workerID int) {
	defer n.batching.wg.Done()
	log.Debug().Int("worker_id", workerID).Msg("Batch worker started")

	batch := make([]*batchItem, 0, n.batching.size)
	timer := time.NewTimer(time.Duration(n.batching.timeoutSeconds) * time.Second)

	for {
		select {
		case <-n.batching.stopChan:
			log.Debug().Int("worker_id", workerID).Msg("Batch worker stopping")
			if len(batch) > 0 {
				n.processBatch(batch)
			}
			return

		case item := <-n.batching.batchChan:
			batch = append(batch, item)

			// Process batch if it's full
			if len(batch) >= n.batching.size {
				n.processBatch(batch)
				batch = make([]*batchItem, 0, n.batching.size)
				timer.Reset(time.Duration(n.batching.timeoutSeconds) * time.Second)
			}

		case <-timer.C:
			// Process batch on timeout if not empty
			if len(batch) > 0 {
				n.processBatch(batch)
				batch = make([]*batchItem, 0, n.batching.size)
			}
			timer.Reset(time.Duration(n.batching.timeoutSeconds) * time.Second)
		}
	}
}

// processBatch handles processing a batch of events
func (n *Notifier) processBatch(batch []*batchItem) {
	if len(batch) == 0 {
		return
	}

	ctx, span := n.tracer.Start(context.Background(), "Notifier.processBatch")
	defer span.End()

	span.SetAttributes(
		attribute.Int("batch_size", len(batch)),
		attribute.String("first_event_type", string(batch[0].event.GetType())),
	)

	log.Debug().
		Int("batch_size", len(batch)).
		Str("first_event_type", string(batch[0].event.GetType())).
		Msg("Processing batch of events")

	// Group events by provider to minimize provider interactions
	n.mu.RLock()
	providers := make([]Provider, len(n.providers))
	copy(providers, n.providers) // Create a copy to avoid holding the lock
	n.mu.RUnlock()

	for _, provider := range providers {
		providerName := provider.Name()
		startTime := time.Now()

		providerCtx, providerSpan := n.tracer.Start(ctx,
			fmt.Sprintf("Provider.%s.BatchSend", providerName),
			trace.WithAttributes(
				attribute.String("provider.name", providerName),
				attribute.Int("batch_size", len(batch)),
			),
		)

		// Track errors for this provider
		errorCount := 0

		// Process all events for this provider
		for _, item := range batch {
			err := provider.Send(item.ctx, item.event)
			if err != nil {
				errorCount++
				log.Error().
					Err(err).
					Str("provider", providerName).
					Str("event_type", string(item.event.GetType())).
					Msg("Failed to send notification in batch")

				n.providerCounter.Add(providerCtx, 1, metric.WithAttributes(
					attribute.String("provider", providerName),
					attribute.String("status", "error"),
					attribute.String("batch", "true"),
				))

				providerSpan.RecordError(err)
			}
		}

		// Record provider batch metrics
		elapsed := time.Since(startTime)
		n.providerDuration.Record(ctx, float64(elapsed.Milliseconds()), metric.WithAttributes(
			attribute.String("provider", providerName),
			attribute.String("mode", "batch"),
		))

		if errorCount > 0 {
			providerSpan.SetStatus(codes.Error, fmt.Sprintf("%d/%d events failed", errorCount, len(batch)))
		} else {
			providerSpan.SetStatus(codes.Ok, "")
		}

		providerSpan.End()
	}

	// Record batch processing metrics
	n.batchProcessingCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.Int("size", len(batch)),
	))
}

// EnableBatching turns on or off event batching
func (n *Notifier) EnableBatching(enabled bool) {
	n.mu.Lock()
	defer n.mu.Unlock()

	// If currently disabled and being enabled
	if !n.batching.enabled && enabled {
		n.batching.enabled = true
		n.StartBatchProcessing()
	} else if n.batching.enabled && !enabled {
		// If currently enabled and being disabled
		n.batching.enabled = false
		n.StopBatchProcessing()
	}
}

// GetNotifierHealth returns a status report about the notifier service
func (n *Notifier) GetNotifierHealth() map[string]interface{} {
	n.mu.RLock()
	defer n.mu.RUnlock()

	// Get DataLake stats if available
	var dlStats *DataLakeStats
	if n.dataLake != nil {
		stats, err := n.dataLake.GetStorageStats(context.Background())
		if err == nil {
			// DataLakeStats is already a struct, not a pointer, so we need to create a pointer to it
			dlStats = &DataLakeStats{
				TotalEvents:       stats.TotalEvents,
				OldestEventTime:   stats.OldestEventTime,
				NewestEventTime:   stats.NewestEventTime,
				StorageUsageBytes: stats.StorageUsageBytes,
				EventCountByType:  stats.EventCountByType,
				EventCountByOrg:   stats.EventCountByOrg,
				AvgEventsPerDay:   stats.AvgEventsPerDay,
			}
		}
	}

	// Build health report
	health := map[string]interface{}{
		"providers": len(n.providers),
		"config": map[string]interface{}{
			"max_events_per_minute": n.config.MaxEventsPerMinute,
			"batch_enabled":         n.batching.enabled,
			"batch_size":            n.batching.size,
			"batch_timeout":         n.batching.timeoutSeconds,
			"retention_days":        n.config.RetentionDays,
		},
		"data_lake_enabled": n.dataLake != nil,
	}

	// Add DataLake stats if available
	if dlStats != nil {
		health["data_lake"] = dlStats
	}

	return health
}

// PurgeOldEvents removes events older than retention policy
func (n *Notifier) PurgeOldEvents(ctx context.Context) error {
	if n.dataLake == nil {
		return fmt.Errorf("no data lake configured")
	}

	ctx, span := n.tracer.Start(ctx, "Notifier.PurgeOldEvents")
	defer span.End()

	log.Info().Int("retention_days", n.config.RetentionDays).Msg("Purging old events")

	count, err := n.dataLake.PurgeByAge(ctx, n.config.RetentionDays)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "Failed to purge old events")
		return fmt.Errorf("failed to purge old events: %w", err)
	}

	log.Info().Int("purged_count", count).Msg("Successfully purged old events")
	return nil
}
