// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"

	"github.com/kopexa-grc/notifier"
	"github.com/rs/zerolog/log"
)

// EmailProvider implements the Provider interface for
// email notifications
type EmailProvider struct {
	// TODO: Add email service dependencies here
	// e.g., SMTP configuration, templates, etc.
}

// NewEmailProvider creates a new email provider
func NewEmailProvider() *EmailProvider {
	return &EmailProvider{}
}

// Name returns the name of the provider
func (e *EmailProvider) Name() string {
	return "email"
}

// Send sends an email notification
func (e *EmailProvider) Send(ctx context.Context, event notifier.BaseEvent) error {
	// If no user IDs are specified, don't send a notification
	userIDs := event.GetUserIDs()
	if len(userIDs) == 0 {
		return nil
	}

	// Get payload as JSON for logging
	payload, _ := event.GetPayloadJSON()

	// TODO: Implement the actual email sending logic
	// 1. Retrieve user information (email addresses)
	// 2. Select email template based on EventType
	// 3. Render email template with payload
	// 4. Send email to each user

	log.Debug().
		Str("event_type", string(event.GetType())).
		Int("user_count", len(userIDs)).
		RawJSON("event_payload", payload).
		Msg("Email notifications would be sent here")

	return nil
}
