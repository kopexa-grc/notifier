# Notifier

[![Go Report Card](https://goreportcard.com/badge/github.com/kopexa-grc/notifier)](https://goreportcard.com/report/github.com/kopexa-grc/notifier)
[![GoDoc](https://godoc.org/github.com/kopexa-grc/notifier?status.svg)](https://godoc.org/github.com/kopexa-grc/notifier)
[![License](https://img.shields.io/github/license/kopexa-grc/notifier)](https://github.com/kopexa-grc/notifier/blob/main/LICENSE)
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/kopexa-grc/notifier/ci.yml?branch=main)

A high-performance, enterprise-grade notification service that enables reliable event processing and notification delivery at scale. Perfect for applications that require robust event handling with advanced resilience mechanisms.

## Features

- **Multi-Provider Support**: Send notifications through various channels (email, SMS, push, etc.) with unified configuration
- **Batch Processing**: Efficiently bundle events for optimal performance and throughput
- **Data Persistence**: Optional persistent storage of events in a Data Lake for audit purposes and analytics
- **Rate Limiting**: Protect against overload through intelligent event processing throttling
- **Resilience Mechanisms**: 
  - Circuit breaker pattern to handle faulty providers
  - Automatic retries for undelivered messages with exponential backoff
- **Multi-Tenant Capability**: Isolated resource management for different tenants
- **Enterprise Features**: Comprehensive monitoring, metrics, and configuration options

## Installation

```bash
go get github.com/kopexa-grc/notifier
```

Requires Go 1.20 or later.

## Quick Start

```go
package main

import (
    "context"
    "time"
    
    "github.com/kopexa-grc/notifier"
)

func main() {
    // Create a new notification service with options
    service, err := notifier.NewService(
        notifier.WithMaxEventsPerMinute(100),
        notifier.WithBatchSize(10),
        notifier.WithCircuitBreakerEnabled(true),
        notifier.WithRetryEnabled(true),
    )
    if err != nil {
        panic(err)
    }
    
    // Notify organization-wide
    service.NotifyOrganization(
        context.Background(),
        notifier.EventTypeInfo,
        "Hello World!",
        "organization-id",
        []string{"user-1", "user-2"},
    )
    
    // Close the service when done
    defer service.Close()
}
```

## Architecture

The Notifier Service is implemented as a pipeline that receives events from various sources, processes them, and distributes them to configured providers. The architecture consists of these main components:

1. **Event Receiver**: Accepts events and forwards them to the processor
2. **Rate Limiter**: Limits the number of processed events per time unit
3. **Batch Processor**: Groups events for efficient processing
4. **Provider Manager**: Manages different notification channels
5. **Resilience Layer**: Implements circuit breaker and retry mechanisms
6. **Data Lake**: Optional persistent storage for events

## Advanced Features

### Rate Limiting

Notifier uses Go's official `golang.org/x/time/rate` package for rate limiting to protect your systems from overload:

```go
// Create a notifier configuration with rate limiting
config := notifier.NotifierConfig{
    MaxEventsPerMinute: 60, // 1 event per second
}

// Adjust rate limit at runtime
service.SetRateLimits(120, 0) // 2 events per second
```

### Circuit Breaker

Protection against cascading failures by temporarily disabling failed providers:

```go
// Circuit Breaker configuration using options pattern
service, err := notifier.NewService(
    notifier.WithCircuitBreakerEnabled(true),
    notifier.WithCircuitBreakerMaxFailures(5),
    notifier.WithCircuitBreakerTimeoutSec(60),
)
```

The Circuit Breaker operates in three states:
- **Closed**: Normal operation, requests pass through
- **Open**: After reaching failure threshold, all requests are rejected
- **Half-open**: After timeout, limited test requests are allowed

### Retry Mechanism

Automatic retry of failed events with exponential backoff strategy:

```go
// Configure retry mechanism
service, err := notifier.NewService(
    notifier.WithRetryEnabled(true),
    notifier.WithMaxRetries(3),
    notifier.WithRetryInitialDelaySec(5),
    notifier.WithRetryMaxDelaySec(60),
    notifier.WithRetryBackoffFactor(2.0),
)
```

### Data Lake Integration

Store events for audit, analysis or manual processing:

```go
// Create a custom data lake implementation
type MyDataLake struct {
    // implementation details
}

func (d *MyDataLake) Store(ctx context.Context, event notifier.BaseEvent) error {
    // Store event implementation
}

// Additional required methods...

// Add data lake to service
service, err := notifier.NewService(
    notifier.WithDatalake(&MyDataLake{}),
)
```

## Configuration Options

### General Configuration

| Option                  | Description                               | Default |
|-------------------------|-------------------------------------------|---------|
| MaxEventsPerMinute      | Rate limiting cap                         | 100     |
| BatchSize               | Number of events per batch                | 10      |
| BatchTimeoutSeconds     | Timeout for batch processing              | 30      |
| RetentionDays           | Data retention period in days             | 90      |

### Resilience Configuration

| Option                    | Description                               | Default |
|---------------------------|-------------------------------------------|---------|
| CircuitBreakerEnabled     | Enables circuit breaker pattern           | false   |
| CircuitBreakerMaxFailures | Failures before tripping                  | 5       |
| CircuitBreakerTimeoutSec  | Reset timeout in seconds                  | 60      |
| RetryEnabled              | Enables retry mechanism                   | false   |
| MaxRetries                | Maximum retry attempts                    | 3       |
| RetryInitialDelaySec      | Initial delay between retries (seconds)   | 5       |
| RetryMaxDelaySec          | Maximum delay between retries (seconds)   | 300     |
| RetryBackoffFactor        | Exponential backoff multiplier            | 2.0     |
| PersistFailedEvents       | Store events after max retries            | false   |

## Provider Implementation

Create custom notification providers by implementing the Provider interface:

```go
type CustomProvider struct {
    // provider fields
}

func (p *CustomProvider) Send(ctx context.Context, event notifier.BaseEvent) error {
    // Implementation for sending notifications
    return nil
}

func (p *CustomProvider) Name() string {
    return "custom-provider"
}

// Register with service
service.RegisterProvider(&CustomProvider{})
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes using Conventional Commits format
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct and development process.

## License

This project is licensed under the BUSL-1.1 License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

* Sony's [gobreaker](https://github.com/sony/gobreaker) library for circuit breaker implementation
* Go's [rate](https://golang.org/x/time/rate) package for rate limiting
