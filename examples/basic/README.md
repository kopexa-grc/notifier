# Grundlegendes Beispiel für den Notifier

Dieses Beispiel zeigt die grundlegende Verwendung des Notifier-Pakets mit zwei verschiedenen Providern:

1. **ConsoleProvider**: Sendet Benachrichtigungen an die Konsole.
2. **FileProvider**: Schreibt Benachrichtigungen in eine Logdatei.

## Struktur des Beispiels

Das Beispiel implementiert:

- Zwei benutzerdefinierte Provider, die das `notifier.Provider`-Interface implementieren
- Eine Beispiel-Anwendung, die den Notifier-Service verwendet, um verschiedene Arten von Benachrichtigungen zu senden

## Ausführen des Beispiels

Um das Beispiel auszuführen:

```bash
cd examples/basic
go run main.go
```

Nach der Ausführung:
- Sie werden Ausgaben auf der Konsole von `ConsoleProvider` sehen
- Eine Datei namens `notifications.log` wird erstellt, die die Ausgaben von `FileProvider` enthält

## Provider-Implementierungen

### ConsoleProvider

Ein einfacher Provider, der Benachrichtigungen auf der Konsole ausgibt. Er zeigt Details wie Event-Typ, Organisations-ID, Space-ID, Payload, Zeitstempel und betroffene Benutzer an.

### FileProvider

Ein Provider, der Benachrichtigungen in eine Logdatei schreibt. Er protokolliert ähnliche Informationen wie der ConsoleProvider, aber in einem kompakteren Format.

## Verwendung im eigenen Projekt

Um den Notifier mit eigenen Providern zu verwenden:

1. Implementieren Sie das `notifier.Provider`-Interface für Ihre individuellen Benachrichtigungskanäle.
2. Erstellen Sie einen Notifier-Service mit `notifier.NewService()` und registrieren Sie Ihre Provider.
3. Verwenden Sie die `Notify*`-Methoden, um Benachrichtigungen zu senden.

## Weitere Möglichkeiten

Dieses Beispiel kann als Startpunkt für erweiterte Verwendungen dienen:

- Hinzufügen von DataLake-Funktionalität zum Speichern und Abfragen vergangener Ereignisse
- Aktivieren von Resilienzfunktionen wie Circuit Breaker und Retry-Mechanismen
- Implementieren von Providern für andere Kanäle wie Email, SMS, Slack, etc.
- Einrichten von Batch-Verarbeitung für effiziente Verarbeitung von Ereignissen 