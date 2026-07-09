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

// InternalUserID returns a stable tenant-scoped user identifier.
func InternalUserID(tenantID, channel, externalUserID string) string {
	return "usr_" + shortHash(tenantID, channel, externalUserID)
}

// UserIDHash returns a low-sensitivity hash for logs and trace attributes.
func UserIDHash(tenantID, channel, userID string) string {
	return "user_hash_" + shortHash(tenantID, channel, userID)
}

// AuditID returns a stable audit identifier for one audit event boundary.
func AuditID(parts ...string) string {
	return "audit_" + shortHash(parts...)
}

// IdempotencyKey returns the canonical duplicate-delivery key.
func IdempotencyKey(tenantID, channel, accountID, platformMessageID string) string {
	return strings.Join([]string{
		"tenant", escapeKeyPart(tenantID),
		"channel", escapeKeyPart(channel),
		"account", escapeKeyPart(accountID),
		"message", escapeKeyPart(platformMessageID),
	}, ":")
}

// SessionIDForInbound returns the stable session id for one inbound message.
func SessionIDForInbound(msg InboundMessage) (string, error) {
	if err := msg.Validate(); err != nil {
		return "", err
	}
	return SessionID(
		msg.TenantID,
		msg.AppID,
		msg.Channel,
		msg.ConversationType,
		msg.ExternalUserID,
		msg.ExternalGroupID,
		msg.ThreadID,
	)
}

// SessionID returns the stable tenant/app/channel-scoped session id.
func SessionID(
	tenantID string,
	appID string,
	channel string,
	conversationType ConversationType,
	externalUserID string,
	externalGroupID string,
	threadID string,
) (string, error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", ErrTenantIDRequired
	}
	if strings.TrimSpace(appID) == "" {
		return "", ErrAppIDRequired
	}
	if strings.TrimSpace(channel) == "" {
		return "", ErrChannelRequired
	}
	prefix := fmt.Sprintf(
		"tenant:%s:app:%s:channel:%s",
		escapeKeyPart(tenantID),
		escapeKeyPart(appID),
		escapeKeyPart(channel),
	)
	switch conversationType {
	case ConversationTypeDM:
		if strings.TrimSpace(externalUserID) == "" {
			return "", ErrExternalUserIDRequired
		}
		return prefix + ":dm:" + escapeKeyPart(externalUserID), nil
	case ConversationTypeGroup:
		if strings.TrimSpace(externalGroupID) == "" {
			return "", ErrExternalGroupIDRequired
		}
		return prefix + ":group:" + escapeKeyPart(externalGroupID), nil
	case ConversationTypeThread:
		if strings.TrimSpace(externalGroupID) == "" {
			return "", ErrExternalGroupIDRequired
		}
		if strings.TrimSpace(threadID) == "" {
			return "", fmt.Errorf("thread_id is required")
		}
		return prefix + ":group:" + escapeKeyPart(externalGroupID) +
			":thread:" + escapeKeyPart(threadID), nil
	case "":
		return "", ErrConversationTypeRequired
	default:
		return "", ErrInvalidConversationType
	}
}

func shortHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:24]
}

func escapeKeyPart(value string) string {
	return url.PathEscape(strings.TrimSpace(value))
}
