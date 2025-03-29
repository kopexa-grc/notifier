# Notifier Service

Ein hochleistungsfähiger Enterprise-grade Benachrichtigungsdienst, der die zuverlässige Verarbeitung von Ereignissen und Benachrichtigungen in großem Maßstab ermöglicht.

## Features

* **Multi-Provider-Unterstützung**: Ermöglicht die gleichzeitige Verwendung verschiedener Benachrichtigungskanäle (E-Mail, SMS, Push, etc.) mit einheitlicher Konfiguration.
* **Batch-Verarbeitung**: Effiziente Bündelung von Ereignissen für optimale Leistung.
* **Datenhaltung**: Wahlweise persistente Speicherung von Ereignissen in einem Data Lake für Audit-Zwecke und Analyse.
* **Rate-Limiting**: Schutz vor Überlastung durch intelligente Begrenzung der Ereignisverarbeitung.
* **Resilienz-Mechanismen**: Circuit Breaker für den Umgang mit fehlerhaften Providern und automatische Wiederholungen für nicht zugestellte Nachrichten.
* **Multi-Tenant-Fähigkeit**: Isolierte Ressourcenverwaltung für verschiedene Mandanten.
* **Enterprise-Features**: Umfassende Überwachung, Metriken und Konfigurationsoptionen für anspruchsvolle Unternehmensanforderungen.

## Architektur

Der Notifier Service ist als Pipeline implementiert, die Ereignisse von verschiedenen Quellen empfängt, diese verarbeitet und an die konfigurierten Provider verteilt. Die Architektur besteht aus folgenden Hauptkomponenten:

1. **Event Receiver**: Nimmt Ereignisse entgegen und leitet sie an den Processor weiter.
2. **Rate Limiter**: Begrenzt die Anzahl der verarbeiteten Ereignisse pro Zeiteinheit.
3. **Batch Processor**: Gruppiert Ereignisse für effiziente Verarbeitung.
4. **Provider Manager**: Verwaltet die verschiedenen Benachrichtigungskanäle.
5. **Resilience Layer**: Implementiert Circuit Breaker und Retry-Mechanismen.
6. **Data Lake**: Optional für die persistente Speicherung von Ereignissen.

## Rate-Limiting mit golang.org/x/time/rate

Der Notifier verwendet die offizielle Go-Bibliothek `golang.org/x/time/rate` für das Rate-Limiting, um die Anzahl der Ereignisse zu begrenzen, die in einem bestimmten Zeitraum verarbeitet werden können. Dies bietet mehrere Vorteile:

### Vorteile von golang.org/x/time/rate

1. **Community-Wartung**: Als Teil der offiziellen Go-Bibliothek wird der Code aktiv gewartet und aktualisiert.
2. **Leistungsoptimierung**: Die Implementierung ist auf hohe Leistung und geringe Ressourcennutzung optimiert.
3. **Reduzierter Wartungsaufwand**: Keine Notwendigkeit, eine eigene Rate-Limiting-Lösung zu pflegen.
4. **Bessere Dokumentation**: Umfassende Dokumentation und weite Verbreitung in der Community.
5. **Zusätzliche Funktionen**: Bietet erweiterte Funktionen wie Burstiness und flexible Ratenkonfiguration.

### Konfigurationsoptionen für Rate-Limiting

| Parameter | Beschreibung | Standardwert |
|-----------|-------------|--------------|
| `MaxEventsPerMinute` | Maximale Anzahl an Ereignissen pro Minute | 100 |

### Beispiel-Nutzung

```go
// Erstellen einer Notifier-Konfiguration mit Rate-Limiting
config := notifier.Config{
    MaxEventsPerMinute: 60, // 1 Ereignis pro Sekunde
}

// Erstellen eines neuen Notifier-Service mit der Konfiguration
n, err := notifier.NewNotifier(config)
if err != nil {
    log.Fatal(err)
}

// Rate-Limit zur Laufzeit anpassen
n.SetMaxEventsPerMinute(120) // 2 Ereignisse pro Sekunde
```

### Fortgeschrittene Nutzung mit golang.org/x/time/rate

Für fortgeschrittene Anwendungsfälle kann die `golang.org/x/time/rate`-Bibliothek direkt verwendet werden:

```go
import "golang.org/x/time/rate"

// Erstellen eines neuen Limiters mit 10 Ereignissen pro Sekunde und einem Burst von 30
limiter := rate.NewLimiter(rate.Limit(10), 30)

// Überprüfen, ob ein Ereignis erlaubt ist
if limiter.Allow() {
    // Ereignis verarbeiten
}

// Alternativ mit Kontext und Timeout
ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
defer cancel()
if err := limiter.Wait(ctx); err == nil {
    // Ereignis verarbeiten
} else {
    // Timeout beim Warten auf Erlaubnis
}
```

## Resilienz-Mechanismen

Der Notifier Service implementiert verschiedene Resilienz-Mechanismen, um mit Fehlern und Störungen umzugehen:

### Circuit Breaker

Schützt das System vor kaskadierenden Ausfällen, indem fehlgeschlagene Provider vorübergehend deaktiviert werden:

```go
// Circuit Breaker Konfiguration
config := notifier.Config{
    CircuitBreakerMaxFailures: 5,
    CircuitBreakerTimeout: 60, // Sekunden
}
```

### Retry-Mechanismus

Automatische Wiederholung fehlgeschlagener Ereignisse mit exponentieller Backoff-Strategie:

```go
// Retry-Konfiguration
config := notifier.Config{
    MaxRetries: 3,
    RetryInitialInterval: 5,  // Sekunden
    RetryMaxInterval: 60,     // Sekunden
    RetryMultiplier: 2.0,     // Exponentieller Faktor
}
```

## Fazit

Der Notifier Service bietet eine robuste und skalierbare Lösung für die Ereignisverarbeitung und Benachrichtigung in Unternehmensanwendungen. Durch die Kombination von effizienter Ressourcennutzung, Resilienz-Mechanismen und umfassenden Konfigurationsoptionen eignet sich der Service ideal für kritische Anwendungen mit hohen Anforderungen an Zuverlässigkeit und Durchsatz.

## Resilience Mechanisms

The Notifier Service is equipped with multiple resilience mechanisms to ensure robust and reliable operation even under adverse conditions:

### Rate Limiting

To prevent overload situations, the Notifier Service utilizes the official Go package `golang.org/x/time/rate` for rate limiting. This mechanism limits the number of events that can be processed per minute, thus protecting both the notifier service and downstream systems.

### Circuit Breaker

The Notifier implements the Circuit Breaker pattern using Sony's `github.com/sony/gobreaker/v2` library. This mechanism temporarily interrupts traffic to a provider when a certain number of failures are detected, preventing faulty components from affecting the entire system.

The Circuit Breaker operates in three states:
- **Closed**: Normal operation, requests are allowed through.
- **Open**: After reaching the failure threshold, all requests are rejected.
- **Half-open**: After a timeout period, a limited number of test requests are allowed.

Benefits of using Sony's gobreaker:
- Proven, community-maintained implementation
- Excellent maintainability and regular updates
- Advanced features like configurable failure detection logic
- Type safety through generics in v2
- Extensive configuration options

### Retry Mechanism

For temporary failures, the Notifier automatically attempts to resend notifications using an exponential backoff strategy with jitter. This maximizes the probability of delivery during temporary network issues or recipient outages.

### DataLake for Undeliverable Events

Events that cannot be delivered even after multiple retry attempts are optionally stored in a DataLake to avoid data loss and enable later analysis or manual processing.

## Configuration

The Notifier Service can be configured in various ways to adapt it to specific requirements:

### General Configuration

```go
type NotifierConfig struct {
    MaxEventsPerMinute  int    // Rate limiting for events per minute
    BatchSize           int    // Size of event batches for processing
    BatchTimeoutSeconds int    // Timeout for batch processing in seconds
    RetentionDays       int    // Retention period for events in the DataLake
}
```

### Resilience Configuration

```go
type ResilienceConfig struct {
    // Circuit Breaker configuration (Sony gobreaker)
    CircuitBreakerEnabled     bool
    CircuitBreakerMaxFailures int
    CircuitBreakerTimeout     time.Duration
    
    // Retry configuration
    RetryEnabled       bool
    MaxRetries         int
    RetryInitialDelay  time.Duration
    RetryMaxDelay      time.Duration
    RetryBackoffFactor float64
    
    // Persistence of failed events
    PersistFailedEvents bool
}
```

## Examples

### Basic Usage with All Resilience Mechanisms

```go
// Create a new notifier with resilience mechanisms
config := notifier.NotifierConfig{
    MaxEventsPerMinute: 100,
    BatchSize: 10,
    BatchTimeoutSeconds: 5,
    RetentionDays: 30,
}

resilience := notifier.DefaultResilienceConfig()
resilience.CircuitBreakerEnabled = true
resilience.CircuitBreakerMaxFailures = 5
resilience.RetryEnabled = true
resilience.MaxRetries = 3
resilience.PersistFailedEvents = true

// Create a notifier with resilience mechanisms
ntr := notifier.NewResilientNotifier(
    config,
    resilience,
    tracer,  // OpenTelemetry tracer for tracing
    meter,   // OpenTelemetry meter for metrics
    emailProvider, slackProvider, // Providers for different channels
)

// Start the notifier
ntr.Start()
defer ntr.Stop()

// Add a DataLake for persistent storage
ntr.SetDataLake(myDataLake)

// Send an event
ctx := context.Background()
event := &MyEvent{
    // ... event data
}
ntr.Notify(ctx, event)
```

## Conclusion

The Notifier Service provides a robust and scalable solution for event notification in large systems. By employing proven libraries like Sony's gobreaker and the official Go rate limiter, reliability is maximized while minimizing maintenance effort. The implemented resilience mechanisms - rate limiting, circuit breaking, retry logic, and persistent storage - make it ideal for critical applications where reliability and fault tolerance are crucial. 