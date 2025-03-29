// Copyright (c) Kopexa GmbH
// SPDX-License-Identifier: BUSL-1.1

package notifier

import (
	"context"
	"encoding/json"
	"time"

	"github.com/centrifugal/centrifuge"
	"github.com/rs/zerolog/log"
)

// CentrifugeProvider implements the Provider interface for
// real-time notifications over websockets using Centrifuge
type CentrifugeProvider struct {
	node *centrifuge.Node
}

// NewCentrifugeProvider creates a new provider for Centrifuge
func NewCentrifugeProvider(node *centrifuge.Node) *CentrifugeProvider {
	return &CentrifugeProvider{
		node: node,
	}
}

// Name returns the name of the provider
func (c *CentrifugeProvider) Name() string {
	return "centrifuge"
}

// Send sends a notification through Centrifuge
func (c *CentrifugeProvider) Send(ctx context.Context, event BaseEvent) error {
	// If no user IDs are specified, don't send a notification
	userIDs := event.GetUserIDs()
	if len(userIDs) == 0 {
		return nil
	}

	// Create a publishable event structure
	publishEvent := struct {
		Type      string          `json:"type"`
		Timestamp string          `json:"timestamp"`
		Payload   json.RawMessage `json:"payload"`
	}{
		Type:      string(event.GetType()),
		Timestamp: event.GetTimestamp().Format(time.RFC3339),
	}

	// Get payload JSON
	payload, err := event.GetPayloadJSON()
	if err != nil {
		log.Error().
			Err(err).
			Str("event_type", string(event.GetType())).
			Msg("Failed to get payload JSON for centrifuge")
		return err
	}
	publishEvent.Payload = payload

	// Convert the event to JSON
	data, err := json.Marshal(publishEvent)
	if err != nil {
		log.Error().
			Err(err).
			Str("event_type", string(event.GetType())).
			Msg("Failed to marshal event for centrifuge")
		return err
	}

	// Send a personal notification to each user
	for _, userID := range userIDs {
		channel := "user:" + userID

		// Publish the notification to the user's personal channel
		_, err := c.node.Publish(channel, data)
		if err != nil {
			log.Error().
				Err(err).
				Str("channel", channel).
				Str("user_id", userID).
				Str("event_type", string(event.GetType())).
				Msg("Failed to publish to centrifuge")

			// Continue processing for other users
			continue
		}

		log.Debug().
			Str("channel", channel).
			Str("user_id", userID).
			Str("event_type", string(event.GetType())).
			Msg("Published notification to centrifuge")
	}

	return nil
}
