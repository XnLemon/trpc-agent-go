//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package channeladapter

import "errors"

var (
	// ErrOutboundNotFound indicates that an outbox item does not exist.
	ErrOutboundNotFound = errors.New("channel adapter outbound not found")
	// ErrOutboundDuplicate indicates that the same outbound dedup key already exists.
	ErrOutboundDuplicate = errors.New("channel adapter outbound duplicate")
	// ErrUnsupportedOutboundKind indicates that a channel cannot deliver the message kind.
	ErrUnsupportedOutboundKind = errors.New("channel adapter unsupported outbound kind")
	// ErrNoProvider indicates that a dispatcher has no provider for a channel.
	ErrNoProvider = errors.New("channel adapter provider not found")
	// ErrOutboundNotClaimed indicates that an outbox item was not claimed for delivery.
	ErrOutboundNotClaimed = errors.New("channel adapter outbound not claimed")
	// ErrOutboundLeaseMismatch indicates that an outbox update used the wrong lease.
	ErrOutboundLeaseMismatch = errors.New("channel adapter outbound lease mismatch")
	// ErrOutboundLeaseExpired indicates that an outbox lease expired before update.
	ErrOutboundLeaseExpired = errors.New("channel adapter outbound lease expired")
	// ErrInvalidDeliveryStatus indicates that a provider returned an invalid status.
	ErrInvalidDeliveryStatus = errors.New("channel adapter invalid delivery status")
	// ErrOutboundReplayNotDeadLetter indicates that only dead-letter records can be replayed.
	ErrOutboundReplayNotDeadLetter = errors.New("channel adapter outbound replay requires dead letter")
)
