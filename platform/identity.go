//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
)

type stableIDPart struct {
	name  string
	value string
}

// InternalUserID returns a stable tenant-scoped user identifier.
func InternalUserID(tenantID, channel, externalUserID string) string {
	return stableID(
		"usr",
		stableIDPart{"tenant", tenantID},
		stableIDPart{"channel", channel},
		stableIDPart{"user", externalUserID},
	)
}

// UserIDHash returns a low-sensitivity hash for logs and trace attributes.
func UserIDHash(tenantID, channel, userID string) string {
	return stableID(
		"user_hash",
		stableIDPart{"tenant", tenantID},
		stableIDPart{"channel", channel},
		stableIDPart{"user", userID},
	)
}

// AuditID returns a stable audit identifier for one audit event boundary.
func AuditID(parts ...string) string {
	return "audit_" + shortHash(parts...)
}

// IdempotencyKey returns the canonical duplicate-delivery key.
func IdempotencyKey(tenantID, channel, accountID, platformMessageID string) string {
	return stableID(
		"idem",
		stableIDPart{"tenant", tenantID},
		stableIDPart{"channel", channel},
		stableIDPart{"account", accountID},
		stableIDPart{"message", platformMessageID},
	)
}

// SessionIDForInbound returns the stable session id for one inbound message.
func SessionIDForInbound(msg InboundMessage) (string, error) {
	if err := msg.Validate(); err != nil {
		return "", err
	}
	parts := []stableIDPart{
		{"tenant", msg.TenantID},
		{"app", msg.AppID},
		{"binding", msg.BindingID},
		{"channel", msg.Channel},
		{"account", msg.ChannelAccountID},
	}
	if msg.MessageType == MessageTypeEvent {
		parts = append(parts,
			stableIDPart{"message_type", string(msg.MessageType)},
			stableIDPart{"event_type", msg.RawEventType},
			stableIDPart{"message", msg.PlatformMessageID},
		)
		return stableID("ses", parts...), nil
	}
	conversationParts, err := sessionConversationParts(
		msg.ConversationType,
		msg.ExternalUserID,
		msg.ExternalGroupID,
		msg.ThreadID,
	)
	if err != nil {
		return "", err
	}
	parts = append(parts, conversationParts...)
	return stableID("ses", parts...), nil
}

// SessionID returns the stable tenant/app/binding/channel/account-scoped session id.
func SessionID(
	tenantID string,
	appID string,
	bindingID string,
	channel string,
	channelAccountID string,
	conversationType ConversationType,
	externalUserID string,
	externalGroupID string,
	threadID string,
) (string, error) {
	if err := validateRoutingIdentifier("tenant_id", tenantID, ErrTenantIDRequired); err != nil {
		return "", err
	}
	if err := validateRoutingIdentifier("app_id", appID, ErrAppIDRequired); err != nil {
		return "", err
	}
	if err := validateRoutingIdentifier("binding_id", bindingID, ErrBindingIDRequired); err != nil {
		return "", err
	}
	if err := validateRoutingIdentifier("channel", channel, ErrChannelRequired); err != nil {
		return "", err
	}
	if err := validateRoutingIdentifier("channel_account_id", channelAccountID, ErrAccountIDRequired); err != nil {
		return "", err
	}
	parts := []stableIDPart{
		{"tenant", tenantID},
		{"app", appID},
		{"binding", bindingID},
		{"channel", channel},
		{"account", channelAccountID},
	}
	conversationParts, err := sessionConversationParts(
		conversationType,
		externalUserID,
		externalGroupID,
		threadID,
	)
	if err != nil {
		return "", err
	}
	parts = append(parts, conversationParts...)
	return stableID("ses", parts...), nil
}

func sessionConversationParts(
	conversationType ConversationType,
	externalUserID string,
	externalGroupID string,
	threadID string,
) ([]stableIDPart, error) {
	switch conversationType {
	case ConversationTypeDM:
		if err := validateRoutingIdentifier("external_user_id", externalUserID, ErrExternalUserIDRequired); err != nil {
			return nil, err
		}
		return []stableIDPart{
			{"conversation_type", string(ConversationTypeDM)},
			{"user", externalUserID},
		}, nil
	case ConversationTypeGroup:
		if err := validateRoutingIdentifier("external_group_id", externalGroupID, ErrExternalGroupIDRequired); err != nil {
			return nil, err
		}
		return []stableIDPart{
			{"conversation_type", string(ConversationTypeGroup)},
			{"group", externalGroupID},
		}, nil
	case ConversationTypeThread:
		if err := validateRoutingIdentifier("external_group_id", externalGroupID, ErrExternalGroupIDRequired); err != nil {
			return nil, err
		}
		if err := validateRoutingIdentifier("thread_id", threadID, fmt.Errorf("thread_id is required")); err != nil {
			return nil, err
		}
		return []stableIDPart{
			{"conversation_type", string(ConversationTypeThread)},
			{"group", externalGroupID},
			{"thread", threadID},
		}, nil
	case "":
		return nil, ErrConversationTypeRequired
	default:
		return nil, ErrInvalidConversationType
	}
}

func stableID(prefix string, parts ...stableIDPart) string {
	hash := sha256.New()
	writeStablePart(hash, "prefix", prefix)
	for _, part := range parts {
		writeStablePart(hash, part.name, part.value)
	}
	return prefix + "_" + hex.EncodeToString(hash.Sum(nil))[:32]
}

func writeStablePart(hash interface{ Write([]byte) (int, error) }, name, value string) {
	fmt.Fprintf(hash, "%d:%s=%d:%s;", len(name), name, len(value), value)
}

func shortHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:24]
}

func escapeKeyPart(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}
