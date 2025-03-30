// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/kopexa-grc/notifier"
)

// ConsoleProvider is a simple provider that sends notifications to the console
type ConsoleProvider struct {
	name string
}

// Name returns the name of the provider
func (p *ConsoleProvider) Name() string {
	return p.name
}

// Send outputs the notification to the console
func (p *ConsoleProvider) Send(ctx context.Context, event notifier.BaseEvent) error {
	tenant := event.GetTenant()
	payload, err := event.GetPayloadJSON()
	if err != nil {
		return err
	}

	fmt.Printf("[CONSOLE] Event Type: %s\n", event.GetType())
	fmt.Printf("[CONSOLE] Organization: %s, Space: %s\n", tenant.OrganizationID, tenant.SpaceID)
	fmt.Printf("[CONSOLE] Payload: %s\n", string(payload))
	fmt.Printf("[CONSOLE] Timestamp: %s\n", event.GetTimestamp().Format(time.RFC3339))
	fmt.Printf("[CONSOLE] Users: %v\n", event.GetUserIDs())
	fmt.Println("-----------------------------------")

	return nil
}

// FileProvider is a provider that writes notifications to a file
type FileProvider struct {
	name     string
	filePath string
	file     *os.File
}

// NewFileProvider creates a new file provider
func NewFileProvider(name, filePath string) (*FileProvider, error) {
	file, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}

	return &FileProvider{
		name:     name,
		filePath: filePath,
		file:     file,
	}, nil
}

// Name returns the name of the provider
func (p *FileProvider) Name() string {
	return p.name
}

// Send writes the notification to a file
func (p *FileProvider) Send(ctx context.Context, event notifier.BaseEvent) error {
	tenant := event.GetTenant()
	payload, err := event.GetPayloadJSON()
	if err != nil {
		return err
	}

	logMessage := fmt.Sprintf(
		"[%s] Event: %s | Org: %s | Space: %s | Users: %v | Payload: %s\n",
		event.GetTimestamp().Format(time.RFC3339),
		event.GetType(),
		tenant.OrganizationID,
		tenant.SpaceID,
		event.GetUserIDs(),
		string(payload),
	)

	_, err = p.file.WriteString(logMessage)
	return err
}

// Close closes the file
func (p *FileProvider) Close() error {
	if p.file != nil {
		return p.file.Close()
	}
	return nil
}

func main() {
	// Create console provider
	consoleProvider := &ConsoleProvider{
		name: "console-provider",
	}

	// Create file provider
	fileProvider, err := NewFileProvider("file-provider", "notifications.log")
	if err != nil {
		log.Fatalf("Error creating file provider: %v", err)
	}
	defer func() {
		_ = fileProvider.Close()
	}()

	// Create notifier service with both providers
	service, err := notifier.NewService(
		notifier.WithProvider(consoleProvider),
		notifier.WithProvider(fileProvider),
		notifier.WithMaxEventsPerMinute(100),
		notifier.WithBatchSize(10),
		notifier.WithBatchTimeoutSeconds(1),
		notifier.WithRetentionDays(30),
	)
	if err != nil {
		log.Fatalf("Error creating notifier service: %v", err)
	}

	// Send a few example notifications
	ctx := context.Background()

	// Example 1: Organization-wide notification
	service.NotifyOrganization(
		ctx,
		notifier.EventTypeInfo,
		map[string]interface{}{
			"message": "This is an organization-wide notification",
			"details": "Important information for all users",
		},
		"org-123",
		[]string{"user1", "user2", "user3"},
	)

	// Example 2: Space-specific notification
	service.NotifySpace(
		ctx,
		notifier.EventTypeWarning,
		map[string]interface{}{
			"message": "This is a space-specific warning",
			"details": "Warning for a specific space",
			"level":   "medium",
		},
		"org-123",
		"space-456",
		[]string{"user1", "user2"},
	)

	// Example 3: Critical notification
	service.NotifySpace(
		ctx,
		notifier.EventTypeCritical,
		map[string]interface{}{
			"message": "Critical system alert",
			"details": "Immediate attention required",
			"code":    "ERR-1234",
		},
		"org-123",
		"space-789",
		[]string{"admin1", "admin2"},
	)

	// Wait a bit for async processing to complete
	time.Sleep(500 * time.Millisecond)

	fmt.Println("All notifications sent successfully!")
	fmt.Println("Check the notifications.log file for the output from the file provider.")
}
