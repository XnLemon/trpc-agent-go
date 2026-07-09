//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package channeladapter

import (
	"context"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/platform"
)

// InboundRequest contains the platform webhook payload after routing to a binding.
type InboundRequest struct {
	Binding      platform.ChannelBinding
	Headers      map[string]string
	Body         []byte
	ReceivedAt   time.Time
	TraceContext map[string]string
}

// InboundParser converts one platform webhook request into a normalized message.
type InboundParser interface {
	ParseInbound(ctx context.Context, req InboundRequest) (platform.InboundMessage, error)
}

// OutboundProvider delivers normalized outbound messages to one IM platform.
type OutboundProvider interface {
	Deliver(ctx context.Context, msg platform.OutboundMessage) (DeliveryResult, error)
}

// Adapter is the channel boundary. It intentionally does not run agents,
// decide governance, or manage long-term memory.
type Adapter interface {
	InboundParser
	OutboundProvider
	Name() string
}

// DeliveryResult describes the provider response for one outbound attempt.
type DeliveryResult struct {
	Status            platform.OutboundStatus
	ProviderMessageID string
	RetryAfter        time.Duration
	Detail            string
}

// TextInbound builds a normalized text message from already-verified channel fields.
func TextInbound(
	binding platform.ChannelBinding,
	platformMessageID string,
	externalUserID string,
	text string,
	receivedAt time.Time,
) platform.InboundMessage {
	return platform.InboundMessage{
		TenantID:          binding.TenantID,
		AppID:             binding.AppID,
		BindingID:         binding.BindingID,
		Channel:           binding.Channel,
		ChannelAccountID:  binding.AccountID,
		PlatformMessageID: platformMessageID,
		ExternalUserID:    externalUserID,
		ConversationType:  platform.ConversationTypeDM,
		MessageType:       platform.MessageTypeText,
		ContentParts: []platform.ContentPart{
			{Type: platform.ContentPartTypeText, Text: text},
		},
		ReceivedAt:      receivedAt,
		SignatureStatus: "verified",
	}
}
