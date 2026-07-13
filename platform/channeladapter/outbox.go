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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/platform"
)

// RetryPolicy controls outbound delivery retry timing.
type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
}

// DefaultRetryPolicy returns a conservative retry policy for IM delivery.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: time.Second,
		MaxBackoff:     time.Minute,
	}
}

// RetryPolicyForBinding builds a retry policy from channel limits when present.
func RetryPolicyForBinding(binding platform.ChannelBinding) RetryPolicy {
	policy := DefaultRetryPolicy()
	if binding.ChannelLimits.RetryMaxAttempts > 0 {
		policy.MaxAttempts = binding.ChannelLimits.RetryMaxAttempts
	}
	return policy
}

// Delay returns the retry delay for the next attempt.
func (p RetryPolicy) Delay(attempt int) time.Duration {
	if p.InitialBackoff <= 0 {
		p.InitialBackoff = time.Second
	}
	if p.MaxBackoff <= 0 {
		p.MaxBackoff = time.Minute
	}
	if attempt <= 1 {
		return p.InitialBackoff
	}
	delay := p.InitialBackoff
	for i := 1; i < attempt; i++ {
		delay *= 2
		if delay >= p.MaxBackoff {
			return p.MaxBackoff
		}
	}
	return delay
}

// OutboxRecord stores delivery state for one outbound message.
type OutboxRecord struct {
	Message           platform.OutboundMessage
	Status            platform.OutboundStatus
	Attempts          int
	MaxAttempts       int
	RetryPolicy       RetryPolicy
	NextAttemptAt     time.Time
	LeaseToken        string
	LeaseExpiresAt    time.Time
	LastError         string
	ProviderMessageID string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	SentAt            *time.Time
}

// OutboxStore stores outbound messages until they are sent or dead-lettered.
type OutboxStore interface {
	Enqueue(ctx context.Context, msg platform.OutboundMessage, policy RetryPolicy) (OutboxRecord, bool, error)
	Get(ctx context.Context, dedupKey string) (OutboxRecord, bool, error)
	ListDue(ctx context.Context, now time.Time, limit int) ([]OutboxRecord, error)
	ClaimDue(ctx context.Context, now time.Time, limit int, leaseDuration time.Duration) ([]OutboxRecord, error)
	MarkSent(ctx context.Context, dedupKey string, leaseToken string, providerMessageID string, now time.Time) (OutboxRecord, error)
	MarkFailed(ctx context.Context, dedupKey string, leaseToken string, err error, now time.Time) (OutboxRecord, error)
	MarkFailedAfter(ctx context.Context, dedupKey string, leaseToken string, err error, now time.Time, retryAfter time.Duration) (OutboxRecord, error)
	MarkDeadLetter(ctx context.Context, dedupKey string, leaseToken string, err error, now time.Time) (OutboxRecord, error)
}

// DeadLetterOutboxStore exposes admin operations for dead-letter inspection and replay.
type DeadLetterOutboxStore interface {
	ListDeadLetters(ctx context.Context, tenantID string, limit int) ([]OutboxRecord, error)
	RequeueDeadLetter(ctx context.Context, dedupKey string, policy RetryPolicy, now time.Time) (OutboxRecord, error)
}

// InMemoryOutboxStore is a concurrency-safe outbox store for tests and demos.
type InMemoryOutboxStore struct {
	mu      sync.Mutex
	records map[string]OutboxRecord
}

// NewInMemoryOutboxStore creates an in-memory outbox store.
func NewInMemoryOutboxStore() *InMemoryOutboxStore {
	return &InMemoryOutboxStore{
		records: make(map[string]OutboxRecord),
	}
}

// Enqueue stores a pending outbound message unless its dedup key already exists.
func (s *InMemoryOutboxStore) Enqueue(
	ctx context.Context,
	msg platform.OutboundMessage,
	policy RetryPolicy,
) (OutboxRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return OutboxRecord{}, false, err
	}
	if msg.DedupKey == "" {
		msg.DedupKey = platform.IdempotencyKey(
			msg.TenantID,
			msg.Channel,
			msg.BindingID,
			msg.ReplyToPlatformMessageID,
		) + fmt.Sprintf(":seq:%d", msg.Sequence)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.records[msg.DedupKey]; ok {
		if !sameOutboundIdentity(existing.Message, msg) {
			return OutboxRecord{}, false, ErrOutboundDuplicate
		}
		return existing, false, nil
	}
	now := time.Now()
	record := OutboxRecord{
		Message:       msg,
		Status:        platform.OutboundStatusPending,
		MaxAttempts:   maxAttempts(policy),
		RetryPolicy:   normalizeRetryPolicy(policy),
		NextAttemptAt: now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	s.records[msg.DedupKey] = record
	return record, true, nil
}

// Get returns one outbox record.
func (s *InMemoryOutboxStore) Get(
	ctx context.Context,
	dedupKey string,
) (OutboxRecord, bool, error) {
	if err := ctx.Err(); err != nil {
		return OutboxRecord{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[dedupKey]
	return record, ok, nil
}

// ListDue returns pending or failed records due for delivery.
func (s *InMemoryOutboxStore) ListDue(
	ctx context.Context,
	now time.Time,
	limit int,
) ([]OutboxRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OutboxRecord, 0)
	for _, record := range s.records {
		if record.Status != platform.OutboundStatusPending &&
			record.Status != platform.OutboundStatusFailed {
			continue
		}
		if record.NextAttemptAt.After(now) {
			continue
		}
		out = append(out, record)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ListDeadLetters returns dead-lettered records, optionally scoped by tenant.
func (s *InMemoryOutboxStore) ListDeadLetters(
	ctx context.Context,
	tenantID string,
	limit int,
) ([]OutboxRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OutboxRecord, 0)
	for _, record := range s.records {
		if record.Status != platform.OutboundStatusDeadLetter {
			continue
		}
		if tenantID != "" && record.Message.TenantID != tenantID {
			continue
		}
		out = append(out, record)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ClaimDue atomically leases due records for one dispatcher worker.
func (s *InMemoryOutboxStore) ClaimDue(
	ctx context.Context,
	now time.Time,
	limit int,
	leaseDuration time.Duration,
) ([]OutboxRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if leaseDuration <= 0 {
		leaseDuration = 30 * time.Second
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]OutboxRecord, 0)
	for key, record := range s.records {
		if record.Status != platform.OutboundStatusPending &&
			record.Status != platform.OutboundStatusFailed {
			continue
		}
		if record.NextAttemptAt.After(now) {
			continue
		}
		if record.LeaseToken != "" && record.LeaseExpiresAt.After(now) {
			continue
		}
		record.LeaseToken = newLeaseToken()
		record.LeaseExpiresAt = now.Add(leaseDuration)
		record.UpdatedAt = now
		s.records[key] = record
		out = append(out, record)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// RequeueDeadLetter moves one dead-lettered record back to pending delivery.
func (s *InMemoryOutboxStore) RequeueDeadLetter(
	ctx context.Context,
	dedupKey string,
	policy RetryPolicy,
	now time.Time,
) (OutboxRecord, error) {
	if err := ctx.Err(); err != nil {
		return OutboxRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[dedupKey]
	if !ok {
		return OutboxRecord{}, ErrOutboundNotFound
	}
	if record.Status != platform.OutboundStatusDeadLetter {
		return OutboxRecord{}, ErrOutboundReplayNotDeadLetter
	}
	record.Status = platform.OutboundStatusPending
	record.Attempts = 0
	record.MaxAttempts = maxAttempts(policy)
	record.RetryPolicy = normalizeRetryPolicy(policy)
	record.NextAttemptAt = now
	record.LeaseToken = ""
	record.LeaseExpiresAt = time.Time{}
	record.LastError = ""
	record.UpdatedAt = now
	s.records[dedupKey] = record
	return record, nil
}

// MarkSent marks an outbox record as delivered.
func (s *InMemoryOutboxStore) MarkSent(
	ctx context.Context,
	dedupKey string,
	leaseToken string,
	providerMessageID string,
	now time.Time,
) (OutboxRecord, error) {
	if err := ctx.Err(); err != nil {
		return OutboxRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[dedupKey]
	if !ok {
		return OutboxRecord{}, ErrOutboundNotFound
	}
	if err := validateLease(record, leaseToken, now); err != nil {
		return OutboxRecord{}, err
	}
	record.Status = platform.OutboundStatusSent
	record.ProviderMessageID = providerMessageID
	record.LeaseToken = ""
	record.LeaseExpiresAt = time.Time{}
	record.UpdatedAt = now
	record.SentAt = &now
	s.records[dedupKey] = record
	return record, nil
}

// MarkFailed records a failed delivery attempt and schedules retry or dead-letter.
func (s *InMemoryOutboxStore) MarkFailed(
	ctx context.Context,
	dedupKey string,
	leaseToken string,
	err error,
	now time.Time,
) (OutboxRecord, error) {
	return s.MarkFailedAfter(ctx, dedupKey, leaseToken, err, now, 0)
}

// MarkFailedAfter records a failed delivery attempt and honors provider retry timing.
func (s *InMemoryOutboxStore) MarkFailedAfter(
	ctx context.Context,
	dedupKey string,
	leaseToken string,
	err error,
	now time.Time,
	retryAfter time.Duration,
) (OutboxRecord, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return OutboxRecord{}, ctxErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[dedupKey]
	if !ok {
		return OutboxRecord{}, ErrOutboundNotFound
	}
	if err := validateLease(record, leaseToken, now); err != nil {
		return OutboxRecord{}, err
	}
	record.Attempts++
	record.LastError = errorString(err)
	record.UpdatedAt = now
	record.RetryPolicy = normalizeRetryPolicy(record.RetryPolicy)
	record.MaxAttempts = maxAttempts(record.RetryPolicy)
	record.LeaseToken = ""
	record.LeaseExpiresAt = time.Time{}
	if record.Attempts >= record.MaxAttempts {
		record.Status = platform.OutboundStatusDeadLetter
		record.NextAttemptAt = time.Time{}
	} else {
		record.Status = platform.OutboundStatusFailed
		record.NextAttemptAt = nextAttemptAt(now, retryAfter, record.RetryPolicy.Delay(record.Attempts))
	}
	s.records[dedupKey] = record
	return record, nil
}

// MarkDeadLetter records a permanent delivery failure without another retry.
func (s *InMemoryOutboxStore) MarkDeadLetter(
	ctx context.Context,
	dedupKey string,
	leaseToken string,
	err error,
	now time.Time,
) (OutboxRecord, error) {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return OutboxRecord{}, ctxErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[dedupKey]
	if !ok {
		return OutboxRecord{}, ErrOutboundNotFound
	}
	if err := validateLease(record, leaseToken, now); err != nil {
		return OutboxRecord{}, err
	}
	record.Attempts++
	record.LastError = errorString(err)
	record.UpdatedAt = now
	record.Status = platform.OutboundStatusDeadLetter
	record.NextAttemptAt = time.Time{}
	record.LeaseToken = ""
	record.LeaseExpiresAt = time.Time{}
	s.records[dedupKey] = record
	return record, nil
}

func maxAttempts(policy RetryPolicy) int {
	if policy.MaxAttempts <= 0 {
		return DefaultRetryPolicy().MaxAttempts
	}
	return policy.MaxAttempts
}

func normalizeRetryPolicy(policy RetryPolicy) RetryPolicy {
	defaultPolicy := DefaultRetryPolicy()
	if policy.MaxAttempts <= 0 {
		policy.MaxAttempts = defaultPolicy.MaxAttempts
	}
	if policy.InitialBackoff <= 0 {
		policy.InitialBackoff = defaultPolicy.InitialBackoff
	}
	if policy.MaxBackoff <= 0 {
		policy.MaxBackoff = defaultPolicy.MaxBackoff
	}
	return policy
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	redactor, redactorErr := platform.NewRedactor()
	if redactorErr != nil {
		return err.Error()
	}
	return redactor.Redact(err.Error())
}

func sameOutboundIdentity(existing platform.OutboundMessage, next platform.OutboundMessage) bool {
	return existing.TenantID == next.TenantID &&
		existing.Channel == next.Channel &&
		existing.BindingID == next.BindingID &&
		existing.ReplyToPlatformMessageID == next.ReplyToPlatformMessageID &&
		existing.Sequence == next.Sequence
}

func nextAttemptAt(now time.Time, retryAfter time.Duration, backoff time.Duration) time.Time {
	delay := backoff
	if retryAfter > delay {
		delay = retryAfter
	}
	return now.Add(delay)
}

func validateLease(record OutboxRecord, leaseToken string, now time.Time) error {
	if record.LeaseToken == "" {
		return ErrOutboundNotClaimed
	}
	if leaseToken == "" || leaseToken != record.LeaseToken {
		return ErrOutboundLeaseMismatch
	}
	if !record.LeaseExpiresAt.IsZero() && !record.LeaseExpiresAt.After(now) {
		return ErrOutboundLeaseExpired
	}
	return nil
}

func newLeaseToken() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf[:])
}
