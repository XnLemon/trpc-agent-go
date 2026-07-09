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
	"errors"
	"fmt"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/platform"
)

// ProviderRegistry resolves outbound providers by channel.
type ProviderRegistry interface {
	ProviderFor(channel string) (OutboundProvider, bool)
}

// ProviderRegistryFunc adapts a function to ProviderRegistry.
type ProviderRegistryFunc func(channel string) (OutboundProvider, bool)

// ProviderFor implements ProviderRegistry.
func (f ProviderRegistryFunc) ProviderFor(channel string) (OutboundProvider, bool) {
	if f == nil {
		return nil, false
	}
	return f(channel)
}

// DispatchResult is the outcome of one due outbox delivery attempt.
type DispatchResult struct {
	DedupKey string
	Status   platform.OutboundStatus
	Error    error
}

// Dispatcher drains due outbound messages and sends them through channel providers.
type Dispatcher struct {
	store     OutboxStore
	providers ProviderRegistry
	policy    RetryPolicy
	now       func() time.Time
	lease     time.Duration
}

// DispatcherOption configures Dispatcher.
type DispatcherOption func(*Dispatcher)

// WithRetryPolicy sets the dispatch retry policy.
func WithRetryPolicy(policy RetryPolicy) DispatcherOption {
	return func(d *Dispatcher) {
		d.policy = policy
	}
}

// WithNow sets the dispatch clock.
func WithNow(now func() time.Time) DispatcherOption {
	return func(d *Dispatcher) {
		if now != nil {
			d.now = now
		}
	}
}

// WithLeaseDuration sets how long one dispatch worker owns claimed records.
func WithLeaseDuration(lease time.Duration) DispatcherOption {
	return func(d *Dispatcher) {
		if lease > 0 {
			d.lease = lease
		}
	}
}

// NewDispatcher creates a due-outbox dispatcher.
func NewDispatcher(
	store OutboxStore,
	providers ProviderRegistry,
	opts ...DispatcherOption,
) *Dispatcher {
	d := &Dispatcher{
		store:     store,
		providers: providers,
		policy:    DefaultRetryPolicy(),
		now:       time.Now,
		lease:     30 * time.Second,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(d)
		}
	}
	return d
}

// DispatchDue sends due outbox messages and updates their delivery state.
func (d *Dispatcher) DispatchDue(ctx context.Context, limit int) ([]DispatchResult, error) {
	if d.store == nil {
		return nil, fmt.Errorf("channel adapter outbox store is required")
	}
	if d.providers == nil {
		return nil, fmt.Errorf("channel adapter providers are required")
	}
	now := d.now()
	due, err := d.store.ClaimDue(ctx, now, limit, d.lease)
	if err != nil {
		return nil, err
	}
	results := make([]DispatchResult, 0, len(due))
	for _, record := range due {
		provider, ok := d.providers.ProviderFor(record.Message.Channel)
		if !ok || provider == nil {
			updated, markErr := d.store.MarkFailed(
				ctx,
				record.Message.DedupKey,
				record.LeaseToken,
				ErrNoProvider,
				now,
			)
			results = append(results, DispatchResult{
				DedupKey: record.Message.DedupKey,
				Status:   updated.Status,
				Error:    markErr,
			})
			continue
		}
		delivery, deliverErr := provider.Deliver(ctx, record.Message)
		if deliverErr != nil {
			updated, markErr := d.store.MarkFailed(
				ctx,
				record.Message.DedupKey,
				record.LeaseToken,
				deliverErr,
				now,
			)
			results = append(results, DispatchResult{
				DedupKey: record.Message.DedupKey,
				Status:   updated.Status,
				Error:    firstErr(markErr, deliverErr),
			})
			continue
		}
		switch delivery.Status {
		case platform.OutboundStatusSent:
			updated, markErr := d.store.MarkSent(
				ctx,
				record.Message.DedupKey,
				record.LeaseToken,
				delivery.ProviderMessageID,
				now,
			)
			results = append(results, DispatchResult{
				DedupKey: record.Message.DedupKey,
				Status:   updated.Status,
				Error:    markErr,
			})
		case platform.OutboundStatusFailed:
			updated, markErr := d.store.MarkFailedAfter(
				ctx,
				record.Message.DedupKey,
				record.LeaseToken,
				deliveryError(delivery),
				now,
				delivery.RetryAfter,
			)
			results = append(results, DispatchResult{
				DedupKey: record.Message.DedupKey,
				Status:   updated.Status,
				Error:    markErr,
			})
		case platform.OutboundStatusDeadLetter:
			updated, markErr := d.store.MarkDeadLetter(
				ctx,
				record.Message.DedupKey,
				record.LeaseToken,
				deliveryError(delivery),
				now,
			)
			results = append(results, DispatchResult{
				DedupKey: record.Message.DedupKey,
				Status:   updated.Status,
				Error:    markErr,
			})
		default:
			updated, markErr := d.store.MarkFailed(
				ctx,
				record.Message.DedupKey,
				record.LeaseToken,
				fmt.Errorf("%w: %q", ErrInvalidDeliveryStatus, delivery.Status),
				now,
			)
			results = append(results, DispatchResult{
				DedupKey: record.Message.DedupKey,
				Status:   updated.Status,
				Error:    markErr,
			})
		}
	}
	return results, nil
}

func deliveryError(delivery DeliveryResult) error {
	if delivery.Detail == "" {
		return errors.New("provider delivery failed")
	}
	return errors.New(delivery.Detail)
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
