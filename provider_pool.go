// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ProviderPool manages a pool of providers to efficiently reuse connections
// and limit the total number of provider instances.
type ProviderPool struct {
	// Maximum number of provider instances allowed per provider type
	maxInstancesPerProvider int
	// Idle timeout for providers (after which they may be removed from the pool)
	idleTimeout time.Duration
	// Mapping of provider type to available provider instances
	providers map[string][]pooledProvider
	// Mutex for thread-safe access to the providers map
	mu sync.RWMutex
	// Provider factory functions
	factories map[string]ProviderFactory
}

// pooledProvider represents a provider instance in the pool
type pooledProvider struct {
	// The actual provider instance
	provider Provider
	// Last time this provider was used
	lastUsed time.Time
}

// ProviderFactory is a function that creates a new provider of a specific type
type ProviderFactory func() (Provider, error)

// PooledProviderAdapter adapts the Provider interface to use a provider pool
type PooledProviderAdapter struct {
	providerType string
	pool         *ProviderPool
}

// NewProviderPool creates a new provider pool with the specified limits
func NewProviderPool(maxInstancesPerProvider int, idleTimeout time.Duration) *ProviderPool {
	if maxInstancesPerProvider <= 0 {
		maxInstancesPerProvider = 10 // Default value
	}

	if idleTimeout <= 0 {
		idleTimeout = 5 * time.Minute // Default value
	}

	pp := &ProviderPool{
		maxInstancesPerProvider: maxInstancesPerProvider,
		idleTimeout:             idleTimeout,
		providers:               make(map[string][]pooledProvider),
		factories:               make(map[string]ProviderFactory),
	}

	// Start background cleanup job
	go pp.startCleanupWorker()

	return pp
}

// RegisterProviderFactory registers a factory function for a specific provider type
func (pp *ProviderPool) RegisterProviderFactory(providerType string, factory ProviderFactory) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	pp.factories[providerType] = factory
	log.Info().Str("provider_type", providerType).Msg("Registered provider factory in pool")
}

// GetProvider retrieves a provider from the pool or creates a new one if necessary
func (pp *ProviderPool) GetProvider(ctx context.Context, providerType string) (Provider, error) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	// Check if we have available providers in the pool
	providers, ok := pp.providers[providerType]
	if ok && len(providers) > 0 {
		// Get the last provider (most recently used for better cache locality)
		provider := providers[len(providers)-1]
		// Update the pool
		pp.providers[providerType] = providers[:len(providers)-1]

		// Update last used time
		provider.lastUsed = time.Now()

		log.Debug().
			Str("provider_type", providerType).
			Int("pool_size", len(pp.providers[providerType])).
			Msg("Retrieved provider from pool")

		return provider.provider, nil
	}

	// No provider available, create a new one
	factory, ok := pp.factories[providerType]
	if !ok {
		log.Error().
			Str("provider_type", providerType).
			Msg("No provider factory registered for this type")
		return nil, ErrProviderNotRegistered
	}

	provider, err := factory()
	if err != nil {
		log.Error().
			Err(err).
			Str("provider_type", providerType).
			Msg("Failed to create new provider instance")
		return nil, err
	}

	log.Debug().
		Str("provider_type", providerType).
		Msg("Created new provider instance")

	return provider, nil
}

// ReleaseProvider returns a provider to the pool for reuse
func (pp *ProviderPool) ReleaseProvider(providerType string, provider Provider) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	// Get current providers of this type
	providers, ok := pp.providers[providerType]
	if !ok {
		providers = []pooledProvider{}
	}

	// Check if we've reached the maximum pool size
	if len(providers) >= pp.maxInstancesPerProvider {
		log.Debug().
			Str("provider_type", providerType).
			Msg("Provider pool is full, discarding provider")
		return
	}

	// Add provider back to the pool
	pp.providers[providerType] = append(providers, pooledProvider{
		provider: provider,
		lastUsed: time.Now(),
	})

	log.Debug().
		Str("provider_type", providerType).
		Int("pool_size", len(pp.providers[providerType])).
		Msg("Provider returned to pool")
}

// startCleanupWorker runs a periodic cleanup of idle providers
func (pp *ProviderPool) startCleanupWorker() {
	ticker := time.NewTicker(pp.idleTimeout / 2)
	defer ticker.Stop()

	for range ticker.C {
		pp.cleanupIdleProviders()
	}
}

// cleanupIdleProviders removes providers that haven't been used for a while
func (pp *ProviderPool) cleanupIdleProviders() {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	now := time.Now()

	for providerType, providers := range pp.providers {
		active := make([]pooledProvider, 0, len(providers))
		removed := 0

		for _, provider := range providers {
			// Keep providers that have been used recently
			if now.Sub(provider.lastUsed) < pp.idleTimeout {
				active = append(active, provider)
			} else {
				removed++
			}
		}

		if removed > 0 {
			log.Debug().
				Str("provider_type", providerType).
				Int("removed_count", removed).
				Msg("Removed idle providers from pool")

			pp.providers[providerType] = active
		}
	}
}

// GetStats returns statistics about the provider pool
func (pp *ProviderPool) GetStats() map[string]interface{} {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	stats := make(map[string]interface{})

	providerStats := make(map[string]int)
	for providerType, providers := range pp.providers {
		providerStats[providerType] = len(providers)
	}

	stats["providers_by_type"] = providerStats
	stats["max_instances_per_provider"] = pp.maxInstancesPerProvider
	stats["idle_timeout_seconds"] = pp.idleTimeout.Seconds()

	return stats
}

// Close shuts down the provider pool and cleans up resources
func (pp *ProviderPool) Close() {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	// Reset the provider maps
	pp.providers = make(map[string][]pooledProvider)
	pp.factories = make(map[string]ProviderFactory)

	log.Info().Msg("Provider pool has been closed")
}

// NewPooledProviderAdapter creates a new adapter for pooled providers
func NewPooledProviderAdapter(providerType string, pool *ProviderPool) *PooledProviderAdapter {
	return &PooledProviderAdapter{
		providerType: providerType,
		pool:         pool,
	}
}

// Send implements the Provider interface by getting a provider from the pool,
// using it, and then returning it to the pool
func (a *PooledProviderAdapter) Send(ctx context.Context, event BaseEvent) error {
	// Get a provider from the pool
	provider, err := a.pool.GetProvider(ctx, a.providerType)
	if err != nil {
		log.Error().
			Err(err).
			Str("provider_type", a.providerType).
			Msg("Failed to get provider from pool")
		return err
	}

	// Use the provider
	err = provider.Send(ctx, event)

	// Return the provider to the pool regardless of success or failure
	a.pool.ReleaseProvider(a.providerType, provider)

	return err
}

// Name returns the name of the provider type
func (a *PooledProviderAdapter) Name() string {
	return a.providerType
}
