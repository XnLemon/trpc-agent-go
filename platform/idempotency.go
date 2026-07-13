//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// IdempotencyStore stores inbound message processing state.
type IdempotencyStore interface {
	// Start records a message as processing if it has not been seen.
	Start(ctx context.Context, record IdempotencyRecord) (IdempotencyRecord, bool, error)
	// Complete marks a processing record as completed.
	Complete(ctx context.Context, key string, resultRef string) (IdempotencyRecord, error)
	// MarkReplyFailed marks a processing or completed record as needing outbound retry.
	MarkReplyFailed(ctx context.Context, key string, resultRef string) (IdempotencyRecord, error)
	// MarkDeadLetter marks a processing record as requiring manual replay.
	MarkDeadLetter(ctx context.Context, key string, resultRef string) (IdempotencyRecord, error)
	// Get returns the record for key.
	Get(ctx context.Context, key string) (IdempotencyRecord, bool, error)
}

// InMemoryIdempotencyStore is a concurrency-safe idempotency store for tests and demos.
type InMemoryIdempotencyStore struct {
	now     func() time.Time
	mu      sync.Mutex
	records map[string]IdempotencyRecord
}

// NewInMemoryIdempotencyStore creates an in-memory idempotency store.
func NewInMemoryIdempotencyStore() *InMemoryIdempotencyStore {
	return &InMemoryIdempotencyStore{
		now:     time.Now,
		records: make(map[string]IdempotencyRecord),
	}
}

// Start records a message as processing if it has not been seen.
func (s *InMemoryIdempotencyStore) Start(
	ctx context.Context,
	record IdempotencyRecord,
) (IdempotencyRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return IdempotencyRecord{}, false, err
	}
	key, err := canonicalIdempotencyKey(record)
	if err != nil {
		return IdempotencyRecord{}, false, err
	}
	if record.IdempotencyKey != "" && record.IdempotencyKey != key {
		return IdempotencyRecord{}, false, fmt.Errorf("idempotency_key does not match canonical key")
	}
	record.IdempotencyKey = key
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.records[key]; ok {
		return existing, false, nil
	}
	now := s.now()
	record.Status = IdempotencyStatusProcessing
	record.FirstSeenAt = now
	record.UpdatedAt = now
	s.records[key] = record
	return record, true, nil
}

// Complete marks a processing record as completed.
func (s *InMemoryIdempotencyStore) Complete(
	ctx context.Context,
	key string,
	resultRef string,
) (IdempotencyRecord, error) {
	return s.update(ctx, key, IdempotencyStatusProcessing, IdempotencyStatusCompleted, resultRef)
}

// MarkReplyFailed marks a processing or completed record as needing outbound retry.
func (s *InMemoryIdempotencyStore) MarkReplyFailed(
	ctx context.Context,
	key string,
	resultRef string,
) (IdempotencyRecord, error) {
	return s.update(ctx, key, "", IdempotencyStatusReplyFailed, resultRef)
}

// MarkDeadLetter marks a processing record as requiring manual replay.
func (s *InMemoryIdempotencyStore) MarkDeadLetter(
	ctx context.Context,
	key string,
	resultRef string,
) (IdempotencyRecord, error) {
	return s.update(ctx, key, IdempotencyStatusProcessing, IdempotencyStatusDeadLetter, resultRef)
}

// Get returns the record for key.
func (s *InMemoryIdempotencyStore) Get(
	ctx context.Context,
	key string,
) (IdempotencyRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return IdempotencyRecord{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[key]
	return record, ok, nil
}

func (s *InMemoryIdempotencyStore) update(
	ctx context.Context,
	key string,
	from IdempotencyStatus,
	status IdempotencyStatus,
	resultRef string,
) (IdempotencyRecord, error) {
	if err := ctx.Err(); err != nil {
		return IdempotencyRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[key]
	if !ok {
		return IdempotencyRecord{}, ErrIdempotencyRecordNotFound
	}
	if from != "" && record.Status != from {
		return IdempotencyRecord{}, fmt.Errorf(
			"invalid idempotency transition from %q to %q",
			record.Status,
			status,
		)
	}
	if from == "" && record.Status != IdempotencyStatusCompleted && record.Status != IdempotencyStatusProcessing {
		return IdempotencyRecord{}, fmt.Errorf(
			"invalid idempotency transition from %q to %q",
			record.Status,
			status,
		)
	}
	record.Status = status
	record.ResultRef = resultRef
	record.UpdatedAt = s.now()
	s.records[key] = record
	return record, nil
}

func canonicalIdempotencyKey(record IdempotencyRecord) (string, error) {
	if err := validateRoutingIdentifier("tenant_id", record.TenantID, ErrTenantIDRequired); err != nil {
		return "", err
	}
	if err := validateRoutingIdentifier("channel", record.Channel, ErrChannelRequired); err != nil {
		return "", err
	}
	if err := validateRoutingIdentifier("account_id", record.AccountID, ErrAccountIDRequired); err != nil {
		return "", err
	}
	if err := validateRoutingIdentifier("platform_message_id", record.PlatformMessageID, ErrPlatformMessageIDRequired); err != nil {
		return "", err
	}
	return IdempotencyKey(
		record.TenantID,
		record.Channel,
		record.AccountID,
		record.PlatformMessageID,
	), nil
}
