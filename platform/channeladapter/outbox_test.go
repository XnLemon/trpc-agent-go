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
	"strings"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/platform"
)

func TestTextInboundUsesBindingBoundary(t *testing.T) {
	binding := testBinding()
	msg := TextInbound(binding, "msg-1", "user-1", "hello", time.Unix(100, 0))

	if msg.TenantID != binding.TenantID ||
		msg.AppID != binding.AppID ||
		msg.BindingID != binding.BindingID ||
		msg.ChannelAccountID != binding.AccountID {
		t.Fatalf("message did not inherit binding boundary: %+v", msg)
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if len(msg.ContentParts) != 1 || msg.ContentParts[0].Text != "hello" {
		t.Fatalf("unexpected content parts: %+v", msg.ContentParts)
	}
}

func TestOutboxEnqueueDeduplicates(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")

	first, inserted, err := store.Enqueue(ctx, msg, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	second, insertedAgain, err := store.Enqueue(ctx, msg, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("enqueue duplicate: %v", err)
	}

	if !inserted || insertedAgain {
		t.Fatalf("expected first insert only, got %v/%v", inserted, insertedAgain)
	}
	if first.Message.DedupKey != second.Message.DedupKey {
		t.Fatalf("duplicate should return existing record")
	}
}

func TestOutboxEnqueueRejectsDedupKeyCollisionAcrossBoundary(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	if _, _, err := store.Enqueue(ctx, msg, DefaultRetryPolicy()); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	colliding := msg
	colliding.TenantID = "other-tenant"

	record, inserted, err := store.Enqueue(ctx, colliding, DefaultRetryPolicy())
	if !errors.Is(err, ErrOutboundDuplicate) {
		t.Fatalf("expected duplicate collision, got record=%+v inserted=%v err=%v", record, inserted, err)
	}
	if inserted {
		t.Fatal("collision must not insert")
	}
	existing, ok, err := store.Get(ctx, msg.DedupKey)
	if err != nil || !ok {
		t.Fatalf("get existing: %v ok=%v", err, ok)
	}
	if existing.Message.TenantID != msg.TenantID {
		t.Fatalf("collision overwrote existing record: %+v", existing)
	}
}

func TestOutboxEnqueueRejectsDedupKeyCollisionAcrossBinding(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	if _, _, err := store.Enqueue(ctx, msg, DefaultRetryPolicy()); err != nil {
		t.Fatalf("enqueue first: %v", err)
	}
	colliding := msg
	colliding.BindingID = "other-binding"

	record, inserted, err := store.Enqueue(ctx, colliding, DefaultRetryPolicy())
	if !errors.Is(err, ErrOutboundDuplicate) {
		t.Fatalf("expected duplicate collision, got record=%+v inserted=%v err=%v", record, inserted, err)
	}
	if inserted {
		t.Fatal("collision must not insert")
	}
	existing, ok, err := store.Get(ctx, msg.DedupKey)
	if err != nil || !ok {
		t.Fatalf("get existing: %v ok=%v", err, ok)
	}
	if existing.Message.BindingID != msg.BindingID {
		t.Fatalf("collision overwrote existing record: %+v", existing)
	}
}

func TestOutboxFailureSchedulesRetryThenDeadLetter(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	policy := RetryPolicy{MaxAttempts: 2, InitialBackoff: time.Second, MaxBackoff: time.Second}
	_, _, err := store.Enqueue(ctx, msg, policy)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	now := time.Now().Add(time.Hour)

	claimed, err := store.ClaimDue(ctx, now, 1, time.Minute)
	if err != nil {
		t.Fatalf("claim due: %v", err)
	}
	failed, err := store.MarkFailed(
		ctx,
		msg.DedupKey,
		claimed[0].LeaseToken,
		errors.New("rate limited"),
		now,
	)
	if err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if failed.Status != platform.OutboundStatusFailed ||
		failed.Attempts != 1 ||
		!failed.NextAttemptAt.Equal(now.Add(time.Second)) {
		t.Fatalf("unexpected failed record: %+v", failed)
	}
	claimed, err = store.ClaimDue(ctx, now.Add(time.Second), 1, time.Minute)
	if err != nil {
		t.Fatalf("claim retry: %v", err)
	}
	dead, err := store.MarkFailed(
		ctx,
		msg.DedupKey,
		claimed[0].LeaseToken,
		errors.New("still failing"),
		now.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("mark dead letter: %v", err)
	}
	if dead.Status != platform.OutboundStatusDeadLetter || dead.Attempts != 2 {
		t.Fatalf("unexpected dead letter record: %+v", dead)
	}
}

func TestOutboxRedactsFailureDetails(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	_, _, err := store.Enqueue(ctx, msg, RetryPolicy{MaxAttempts: 2})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	now := time.Now().Add(time.Hour)
	claimed, err := store.ClaimDue(ctx, now, 1, time.Minute)
	if err != nil {
		t.Fatalf("claim due: %v", err)
	}

	failed, err := store.MarkFailed(
		ctx,
		msg.DedupKey,
		claimed[0].LeaseToken,
		errors.New("provider failed Authorization: Bearer raw-token postgres://user:pass@example/db api_key=sk-1234567890abcdef"),
		now,
	)
	if err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	for _, leaked := range []string{"raw-token", ":pass@", "sk-1234567890abcdef"} {
		if strings.Contains(failed.LastError, leaked) {
			t.Fatalf("failure detail leaked %q: %q", leaked, failed.LastError)
		}
	}
	if !strings.Contains(failed.LastError, "Authorization: ****") ||
		!strings.Contains(failed.LastError, "user:****@") ||
		!strings.Contains(failed.LastError, "api_key=****") {
		t.Fatalf("expected redacted diagnostic, got %q", failed.LastError)
	}
}

func TestOutboxRedactsDeadLetterDetails(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	_, _, err := store.Enqueue(ctx, msg, RetryPolicy{MaxAttempts: 1})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	now := time.Now().Add(time.Hour)
	claimed, err := store.ClaimDue(ctx, now, 1, time.Minute)
	if err != nil {
		t.Fatalf("claim due: %v", err)
	}

	dead, err := store.MarkDeadLetter(
		ctx,
		msg.DedupKey,
		claimed[0].LeaseToken,
		errors.New("permanent token=secret-value cookie=session-secret"),
		now,
	)
	if err != nil {
		t.Fatalf("mark dead letter: %v", err)
	}

	for _, leaked := range []string{"secret-value", "session-secret"} {
		if strings.Contains(dead.LastError, leaked) {
			t.Fatalf("dead-letter detail leaked %q: %q", leaked, dead.LastError)
		}
	}
	if !strings.Contains(dead.LastError, "token=****") ||
		!strings.Contains(dead.LastError, "cookie=****") {
		t.Fatalf("expected redacted diagnostic, got %q", dead.LastError)
	}
}

func TestOutboxListsAndRequeuesDeadLetter(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	policy := RetryPolicy{MaxAttempts: 1, InitialBackoff: time.Second, MaxBackoff: time.Second}
	_, _, err := store.Enqueue(ctx, msg, policy)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	now := time.Now().Add(time.Hour)
	claimed, err := store.ClaimDue(ctx, now, 1, time.Minute)
	if err != nil {
		t.Fatalf("claim due: %v", err)
	}
	_, err = store.MarkDeadLetter(ctx, msg.DedupKey, claimed[0].LeaseToken, errors.New("permanent"), now)
	if err != nil {
		t.Fatalf("mark dead letter: %v", err)
	}

	dead, err := store.ListDeadLetters(ctx, "tenant", 10)
	if err != nil {
		t.Fatalf("ListDeadLetters: %v", err)
	}
	if len(dead) != 1 || dead[0].Message.DedupKey != msg.DedupKey {
		t.Fatalf("unexpected dead letters: %+v", dead)
	}
	otherTenant, err := store.ListDeadLetters(ctx, "other-tenant", 10)
	if err != nil {
		t.Fatalf("ListDeadLetters other tenant: %v", err)
	}
	if len(otherTenant) != 0 {
		t.Fatalf("dead letter listing should respect tenant scope: %+v", otherTenant)
	}

	requeued, err := store.RequeueDeadLetter(ctx, msg.DedupKey, RetryPolicy{
		MaxAttempts:    3,
		InitialBackoff: 2 * time.Second,
		MaxBackoff:     10 * time.Second,
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("RequeueDeadLetter: %v", err)
	}
	if requeued.Status != platform.OutboundStatusPending ||
		requeued.Attempts != 0 ||
		requeued.MaxAttempts != 3 ||
		requeued.LastError != "" ||
		!requeued.NextAttemptAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("unexpected requeued record: %+v", requeued)
	}
	due, err := store.ClaimDue(ctx, now.Add(time.Minute), 1, time.Minute)
	if err != nil {
		t.Fatalf("claim requeued: %v", err)
	}
	if len(due) != 1 || due[0].Message.DedupKey != msg.DedupKey {
		t.Fatalf("requeued record should be due: %+v", due)
	}
}

func TestOutboxRequeueRejectsNonDeadLetter(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	_, _, err := store.Enqueue(ctx, msg, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	record, err := store.RequeueDeadLetter(ctx, msg.DedupKey, DefaultRetryPolicy(), time.Now())
	if !errors.Is(err, ErrOutboundReplayNotDeadLetter) {
		t.Fatalf("expected replay rejection, got record=%+v err=%v", record, err)
	}
}

func TestOutboxRequeueRejectsSentRecord(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	_, _, err := store.Enqueue(ctx, msg, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	now := time.Now().Add(time.Hour)
	claimed, err := store.ClaimDue(ctx, now, 1, time.Minute)
	if err != nil {
		t.Fatalf("claim due: %v", err)
	}
	_, err = store.MarkSent(ctx, msg.DedupKey, claimed[0].LeaseToken, "provider-1", now)
	if err != nil {
		t.Fatalf("mark sent: %v", err)
	}

	record, err := store.RequeueDeadLetter(ctx, msg.DedupKey, DefaultRetryPolicy(), now)
	if !errors.Is(err, ErrOutboundReplayNotDeadLetter) {
		t.Fatalf("expected replay rejection, got record=%+v err=%v", record, err)
	}
}

func TestDispatcherMarksSent(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	_, _, err := store.Enqueue(ctx, msg, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	provider := &fakeProvider{result: DeliveryResult{
		Status:            platform.OutboundStatusSent,
		ProviderMessageID: "provider-1",
	}}
	dispatcher := NewDispatcher(
		store,
		ProviderRegistryFunc(func(channel string) (OutboundProvider, bool) {
			return provider, channel == "telegram"
		}),
		WithNow(func() time.Time { return time.Now().Add(time.Hour) }),
	)

	results, err := dispatcher.DispatchDue(ctx, 10)
	if err != nil {
		t.Fatalf("DispatchDue: %v", err)
	}
	if len(results) != 1 || results[0].Status != platform.OutboundStatusSent {
		t.Fatalf("unexpected dispatch results: %+v", results)
	}
	record, ok, err := store.Get(ctx, msg.DedupKey)
	if err != nil || !ok {
		t.Fatalf("get sent record: %v ok=%v", err, ok)
	}
	if record.ProviderMessageID != "provider-1" || record.SentAt == nil {
		t.Fatalf("unexpected sent record: %+v", record)
	}
	if len(provider.messages) != 1 || provider.messages[0].DedupKey != msg.DedupKey {
		t.Fatalf("provider did not receive message: %+v", provider.messages)
	}
}

func TestDispatcherRetriesFailureWithoutRerunningAgent(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	policy := RetryPolicy{MaxAttempts: 2, InitialBackoff: time.Second, MaxBackoff: time.Second}
	_, _, err := store.Enqueue(ctx, msg, policy)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	provider := &fakeProvider{err: errors.New("temporary failure")}
	now := time.Now().Add(time.Hour)
	dispatcher := NewDispatcher(
		store,
		ProviderRegistryFunc(func(channel string) (OutboundProvider, bool) { return provider, true }),
		WithRetryPolicy(policy),
		WithNow(func() time.Time { return now }),
	)

	results, err := dispatcher.DispatchDue(ctx, 10)
	if err != nil {
		t.Fatalf("DispatchDue: %v", err)
	}
	if len(results) != 1 || results[0].Status != platform.OutboundStatusFailed {
		t.Fatalf("unexpected first dispatch: %+v", results)
	}
	due, err := store.ListDue(ctx, now, 10)
	if err != nil {
		t.Fatalf("ListDue before retry: %v", err)
	}
	if len(due) != 0 {
		t.Fatalf("retry should not be immediately due: %+v", due)
	}

	now = now.Add(time.Second)
	results, err = dispatcher.DispatchDue(ctx, 10)
	if err != nil {
		t.Fatalf("DispatchDue retry: %v", err)
	}
	if len(results) != 1 || results[0].Status != platform.OutboundStatusDeadLetter {
		t.Fatalf("unexpected retry dispatch: %+v", results)
	}
	if len(provider.messages) != 2 {
		t.Fatalf("expected outbound retries only, got %d provider calls", len(provider.messages))
	}
}

func TestDispatcherUsesRecordRetryPolicy(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	_, _, err := store.Enqueue(ctx, msg, RetryPolicy{
		MaxAttempts:    1,
		InitialBackoff: time.Second,
		MaxBackoff:     time.Second,
	})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	provider := &fakeProvider{err: errors.New("temporary failure")}
	dispatcher := NewDispatcher(
		store,
		ProviderRegistryFunc(func(channel string) (OutboundProvider, bool) { return provider, true }),
		WithNow(func() time.Time { return time.Now().Add(time.Hour) }),
	)

	results, err := dispatcher.DispatchDue(ctx, 10)
	if err != nil {
		t.Fatalf("DispatchDue: %v", err)
	}
	if len(results) != 1 || results[0].Status != platform.OutboundStatusDeadLetter {
		t.Fatalf("record retry policy should force dead-letter: %+v", results)
	}
	record, ok, err := store.Get(ctx, msg.DedupKey)
	if err != nil || !ok {
		t.Fatalf("get record: %v ok=%v", err, ok)
	}
	if record.MaxAttempts != 1 || record.Status != platform.OutboundStatusDeadLetter {
		t.Fatalf("dispatcher should not override record policy: %+v", record)
	}
}

func TestDispatcherRejectsInvalidProviderStatus(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	policy := RetryPolicy{MaxAttempts: 2, InitialBackoff: time.Second, MaxBackoff: time.Second}
	_, _, err := store.Enqueue(ctx, msg, policy)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	dispatcher := NewDispatcher(
		store,
		ProviderRegistryFunc(func(channel string) (OutboundProvider, bool) {
			return &fakeProvider{result: DeliveryResult{Status: platform.OutboundStatusPending}}, true
		}),
		WithRetryPolicy(policy),
		WithNow(func() time.Time { return time.Now().Add(time.Hour) }),
	)

	results, err := dispatcher.DispatchDue(ctx, 10)
	if err != nil {
		t.Fatalf("DispatchDue: %v", err)
	}
	if len(results) != 1 || results[0].Status != platform.OutboundStatusFailed {
		t.Fatalf("unexpected dispatch results: %+v", results)
	}
	record, ok, err := store.Get(ctx, msg.DedupKey)
	if err != nil || !ok {
		t.Fatalf("get record: %v ok=%v", err, ok)
	}
	if record.Status != platform.OutboundStatusFailed ||
		record.LastError != `channel adapter invalid delivery status: "pending"` {
		t.Fatalf("invalid status should be failed with diagnostic: %+v", record)
	}
}

func TestDispatcherHonorsProviderRetryAfter(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	policy := RetryPolicy{MaxAttempts: 2, InitialBackoff: time.Second, MaxBackoff: time.Second}
	_, _, err := store.Enqueue(ctx, msg, policy)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	now := time.Now().Add(time.Hour)
	dispatcher := NewDispatcher(
		store,
		ProviderRegistryFunc(func(channel string) (OutboundProvider, bool) {
			return &fakeProvider{result: DeliveryResult{
				Status:     platform.OutboundStatusFailed,
				RetryAfter: 10 * time.Second,
				Detail:     "rate limited",
			}}, true
		}),
		WithRetryPolicy(policy),
		WithNow(func() time.Time { return now }),
	)

	results, err := dispatcher.DispatchDue(ctx, 10)
	if err != nil {
		t.Fatalf("DispatchDue: %v", err)
	}
	if len(results) != 1 || results[0].Status != platform.OutboundStatusFailed {
		t.Fatalf("unexpected dispatch results: %+v", results)
	}
	record, ok, err := store.Get(ctx, msg.DedupKey)
	if err != nil || !ok {
		t.Fatalf("get record: %v ok=%v", err, ok)
	}
	if !record.NextAttemptAt.Equal(now.Add(10 * time.Second)) {
		t.Fatalf("retry-after should drive next attempt, got %+v", record)
	}
}

func TestDispatcherDeadLettersPermanentProviderFailure(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	_, _, err := store.Enqueue(ctx, msg, RetryPolicy{MaxAttempts: 5})
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	dispatcher := NewDispatcher(
		store,
		ProviderRegistryFunc(func(channel string) (OutboundProvider, bool) {
			return &fakeProvider{result: DeliveryResult{
				Status: platform.OutboundStatusDeadLetter,
				Detail: ErrUnsupportedOutboundKind.Error(),
			}}, true
		}),
		WithNow(func() time.Time { return time.Now().Add(time.Hour) }),
	)

	results, err := dispatcher.DispatchDue(ctx, 10)
	if err != nil {
		t.Fatalf("DispatchDue: %v", err)
	}
	if len(results) != 1 || results[0].Status != platform.OutboundStatusDeadLetter {
		t.Fatalf("unexpected dispatch results: %+v", results)
	}
	record, ok, err := store.Get(ctx, msg.DedupKey)
	if err != nil || !ok {
		t.Fatalf("get record: %v ok=%v", err, ok)
	}
	if record.Status != platform.OutboundStatusDeadLetter || record.NextAttemptAt != (time.Time{}) {
		t.Fatalf("permanent failure should not retry: %+v", record)
	}
}

func TestDispatcherClaimsDueRecordsOnce(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryOutboxStore()
	msg := outbound("reply-1")
	_, _, err := store.Enqueue(ctx, msg, DefaultRetryPolicy())
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	provider := &fakeProvider{block: make(chan struct{}), result: DeliveryResult{
		Status: platform.OutboundStatusSent,
	}}
	dispatcher := NewDispatcher(
		store,
		ProviderRegistryFunc(func(channel string) (OutboundProvider, bool) { return provider, true }),
		WithNow(func() time.Time { return time.Now().Add(time.Hour) }),
		WithLeaseDuration(time.Minute),
	)
	firstDone := make(chan error, 1)
	go func() {
		_, err := dispatcher.DispatchDue(ctx, 10)
		firstDone <- err
	}()
	provider.waitForCall(t)

	results, err := dispatcher.DispatchDue(ctx, 10)
	if err != nil {
		t.Fatalf("second DispatchDue: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("leased record should not be dispatched twice: %+v", results)
	}
	close(provider.block)
	if err := <-firstDone; err != nil {
		t.Fatalf("first DispatchDue: %v", err)
	}
	if len(provider.messages) != 1 {
		t.Fatalf("provider should receive one delivery, got %d", len(provider.messages))
	}
}

func testBinding() platform.ChannelBinding {
	return platform.ChannelBinding{
		TenantID:    "tenant",
		AppID:       "app",
		BindingID:   "binding",
		Channel:     "telegram",
		AccountID:   "bot",
		WebhookPath: "/channels/telegram/binding/callback",
		TokenRef:    "secret://telegram-token",
		Status:      platform.BindingStatusActive,
	}
}

func outbound(dedupKey string) platform.OutboundMessage {
	return platform.OutboundMessage{
		TenantID:                 "tenant",
		BindingID:                "binding",
		Channel:                  "telegram",
		SessionID:                "session",
		ReplyToPlatformMessageID: "msg-1",
		Kind:                     platform.OutboundMessageKindText,
		Content:                  "hello",
		Sequence:                 1,
		DedupKey:                 dedupKey,
		TraceID:                  "trace",
	}
}

type fakeProvider struct {
	result   DeliveryResult
	err      error
	messages []platform.OutboundMessage
	block    chan struct{}
	called   chan struct{}
}

func (p *fakeProvider) Deliver(
	ctx context.Context,
	msg platform.OutboundMessage,
) (DeliveryResult, error) {
	if err := ctx.Err(); err != nil {
		return DeliveryResult{}, err
	}
	if p.called == nil {
		p.called = make(chan struct{})
	}
	p.messages = append(p.messages, msg)
	select {
	case <-p.called:
	default:
		close(p.called)
	}
	if p.block != nil {
		<-p.block
	}
	if p.err != nil {
		return DeliveryResult{}, p.err
	}
	return p.result, nil
}

func (p *fakeProvider) waitForCall(t *testing.T) {
	t.Helper()
	if p.called == nil {
		p.called = make(chan struct{})
	}
	select {
	case <-p.called:
	case <-time.After(time.Second):
		t.Fatal("provider was not called")
	}
}
