//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package gateway

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/platform/channeladapter"
)

func TestServiceHandleInboundIsolatesTenants(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	runnerA := &recordingRunner{response: "alpha"}
	runnerB := &recordingRunner{response: "beta"}
	registerRuntime(t, registry, "tenant-a", runnerA)
	registerRuntime(t, registry, "tenant-b", runnerB)
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
	)

	resultA, err := svc.HandleInbound(ctx, inbound("tenant-a", "msg-1", "shared-user", "hello"))
	require.NoError(t, err)
	resultB, err := svc.HandleInbound(ctx, inbound("tenant-b", "msg-1", "shared-user", "hello"))
	require.NoError(t, err)

	assert.NotEqual(t, resultA.SessionID, resultB.SessionID)
	assert.NotEqual(t, runnerA.calls[0].userID, runnerB.calls[0].userID)
	assert.Equal(t, "alpha", resultA.Outbound.Content)
	assert.Equal(t, "beta", resultB.Outbound.Content)
}

func TestServiceHandleInboundDeduplicatesPlatformMessage(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "first"}
	registerRuntime(t, registry, "tenant-a", r)
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")

	first, err := svc.HandleInbound(ctx, msg)
	require.NoError(t, err)
	second, err := svc.HandleInbound(ctx, msg)
	require.NoError(t, err)

	assert.False(t, first.Duplicate)
	assert.True(t, second.Duplicate)
	assert.False(t, second.Processing)
	assert.Equal(t, first.Outbound, second.Outbound)
	assert.Len(t, r.calls, 1)
}

func TestServiceHandleInboundEnqueuesChannelOutbox(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "queued"}
	runtime := validRuntime("tenant-a", r)
	runtime.Binding.ChannelLimits.RetryMaxAttempts = 5
	require.NoError(t, registry.Register(runtime))
	outbox := channeladapter.NewInMemoryOutboxStore()
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewOutboxBackedOutboundStore(outbox),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")

	result, err := svc.HandleInbound(ctx, msg)
	require.NoError(t, err)

	record, ok, err := outbox.Get(ctx, result.Outbound.DedupKey)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, platform.OutboundStatusPending, record.Status)
	assert.Equal(t, result.Outbound, record.Message)
	assert.Equal(t, 5, record.MaxAttempts)
}

func TestServiceHandleInboundDispatchesOutboundToProvider(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "telegram reply"}
	require.NoError(t, registry.Register(validRuntimeForBinding(
		"tenant-a",
		"app-telegram",
		"binding-telegram",
		"telegram",
		"acct-telegram",
		r,
	)))
	outbox := channeladapter.NewInMemoryOutboxStore()
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewOutboxBackedOutboundStore(outbox),
	)
	msg := inboundForRuntime(
		"tenant-a",
		"app-telegram",
		"binding-telegram",
		"telegram",
		"acct-telegram",
		"msg-telegram-dm",
		"user-1",
		"hello telegram",
	)

	result, err := svc.HandleInbound(ctx, msg)
	require.NoError(t, err)
	provider := &recordingOutboundProvider{
		status:            platform.OutboundStatusSent,
		providerMessageID: "telegram-provider-msg-1",
	}
	dispatcher := channeladapter.NewDispatcher(
		outbox,
		channeladapter.ProviderRegistryFunc(func(channel string) (channeladapter.OutboundProvider, bool) {
			if channel != "telegram" {
				return nil, false
			}
			return provider, true
		}),
	)

	dispatchResults, err := dispatcher.DispatchDue(ctx, 10)
	require.NoError(t, err)

	require.Len(t, dispatchResults, 1)
	assert.Equal(t, result.Outbound.DedupKey, dispatchResults[0].DedupKey)
	assert.Equal(t, platform.OutboundStatusSent, dispatchResults[0].Status)
	assert.NoError(t, dispatchResults[0].Error)
	require.Len(t, provider.delivered, 1)
	delivered := provider.delivered[0]
	expectedDedupKey := platform.IdempotencyKey(
		"tenant-a",
		"telegram",
		"acct-telegram",
		"msg-telegram-dm",
	) + ":outbound:1"
	assert.Equal(t, "tenant-a", delivered.TenantID)
	assert.Equal(t, "binding-telegram", delivered.BindingID)
	assert.Equal(t, "telegram", delivered.Channel)
	assert.Equal(t, result.SessionID, delivered.SessionID)
	assert.Equal(t, "msg-telegram-dm", delivered.ReplyToPlatformMessageID)
	assert.Equal(t, platform.OutboundMessageKindText, delivered.Kind)
	assert.Equal(t, "telegram reply", delivered.Content)
	assert.Equal(t, 1, delivered.Sequence)
	assert.Equal(t, expectedDedupKey, delivered.DedupKey)
	assert.Equal(t, result.RequestID, delivered.TraceID)
	assert.Equal(t, result.Outbound, delivered)
	record, ok, err := outbox.Get(ctx, result.Outbound.DedupKey)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, platform.OutboundStatusSent, record.Status)
	assert.Equal(t, "telegram-provider-msg-1", record.ProviderMessageID)
	assert.NotNil(t, record.SentAt)
	assert.Len(t, r.calls, 1)
}

func TestServiceHandleInboundCoversMinimumLoopAcceptance(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	wecomRunner := &recordingRunner{response: "wecom reply"}
	telegramRunner := &recordingRunner{response: "telegram reply"}
	require.NoError(t, registry.Register(validRuntimeForBinding(
		"tenant-a",
		"app-wecom",
		"binding-wecom",
		"wecom",
		"acct-wecom",
		wecomRunner,
	)))
	require.NoError(t, registry.Register(validRuntimeForBinding(
		"tenant-b",
		"app-telegram",
		"binding-telegram",
		"telegram",
		"acct-telegram",
		telegramRunner,
	)))
	require.NoError(t, registry.Register(validRuntimeForBinding(
		"tenant-b",
		"app-wecom",
		"binding-wecom-tenant-b",
		"wecom",
		"acct-wecom",
		wecomRunner,
	)))
	idempotency := platform.NewInMemoryIdempotencyStore()
	outbox := channeladapter.NewInMemoryOutboxStore()
	audit := platform.NewInMemoryAuditSink()
	svc := NewService(
		registry,
		idempotency,
		NewOutboxBackedOutboundStore(outbox),
		WithAuditSink(audit),
	)
	wecomDM := inboundForRuntime(
		"tenant-a",
		"app-wecom",
		"binding-wecom",
		"wecom",
		"acct-wecom",
		"msg-wecom-dm",
		"user-shared",
		"hello wecom",
	)
	telegramDM := inboundForRuntime(
		"tenant-b",
		"app-telegram",
		"binding-telegram",
		"telegram",
		"acct-telegram",
		"msg-telegram-dm",
		"user-shared",
		"hello telegram",
	)
	tenantBWeComDM := inboundForRuntime(
		"tenant-b",
		"app-wecom",
		"binding-wecom-tenant-b",
		"wecom",
		"acct-wecom",
		"msg-tenant-b-wecom-dm",
		"user-shared",
		"hello same channel",
	)
	wecomGroup := inboundForRuntime(
		"tenant-a",
		"app-wecom",
		"binding-wecom",
		"wecom",
		"acct-wecom",
		"msg-wecom-group",
		"user-shared",
		"hello group",
	)
	wecomGroup.ConversationType = platform.ConversationTypeGroup
	wecomGroup.ExternalGroupID = "room-1"

	wecomResult, err := svc.HandleInbound(ctx, wecomDM)
	require.NoError(t, err)
	telegramResult, err := svc.HandleInbound(ctx, telegramDM)
	require.NoError(t, err)
	tenantBWeComResult, err := svc.HandleInbound(ctx, tenantBWeComDM)
	require.NoError(t, err)
	groupResult, err := svc.HandleInbound(ctx, wecomGroup)
	require.NoError(t, err)
	duplicate, err := svc.HandleInbound(ctx, wecomDM)
	require.NoError(t, err)

	assert.False(t, wecomResult.Duplicate)
	assert.True(t, duplicate.Duplicate)
	assert.Equal(t, wecomResult.Outbound, duplicate.Outbound)
	assert.Len(t, wecomRunner.calls, 3)
	assert.Len(t, telegramRunner.calls, 1)
	assert.Equal(t, "wecom reply", wecomResult.Outbound.Content)
	assert.Equal(t, "telegram reply", telegramResult.Outbound.Content)
	assert.Equal(t, "wecom reply", tenantBWeComResult.Outbound.Content)
	assert.Equal(t, "wecom reply", groupResult.Outbound.Content)
	wantWeComSessionID, err := platform.SessionIDForInbound(wecomDM)
	require.NoError(t, err)
	wantTelegramSessionID, err := platform.SessionIDForInbound(telegramDM)
	require.NoError(t, err)
	wantTenantBWeComSessionID, err := platform.SessionIDForInbound(tenantBWeComDM)
	require.NoError(t, err)
	wantGroupSessionID, err := platform.SessionIDForInbound(wecomGroup)
	require.NoError(t, err)
	assert.Equal(t, wantWeComSessionID, wecomResult.SessionID)
	assert.Equal(t, wantTelegramSessionID, telegramResult.SessionID)
	assert.Equal(t, wantTenantBWeComSessionID, tenantBWeComResult.SessionID)
	assert.Equal(t, wantGroupSessionID, groupResult.SessionID)
	assert.NotContains(t, wecomResult.SessionID, "user-shared")
	assert.NotContains(t, wecomResult.SessionID, ":")
	assert.NotEqual(t, wecomResult.SessionID, telegramResult.SessionID)
	assert.NotEqual(t, wecomResult.SessionID, tenantBWeComResult.SessionID)
	assert.NotEqual(t, wecomResult.SessionID, groupResult.SessionID)
	assert.NotEqual(t, wecomRunner.calls[0].userID, telegramRunner.calls[0].userID)
	assert.NotEqual(t, wecomRunner.calls[0].userID, wecomRunner.calls[1].userID)
	assertRunnerCall(t, wecomRunner.calls[0], wecomResult, wecomDM, "hello wecom")
	assertRunnerCall(t, telegramRunner.calls[0], telegramResult, telegramDM, "hello telegram")
	assertRunnerCall(t, wecomRunner.calls[1], tenantBWeComResult, tenantBWeComDM, "hello same channel")
	assertRunnerCall(t, wecomRunner.calls[2], groupResult, wecomGroup, "hello group")

	due, err := outbox.ListDue(ctx, time.Now().Add(time.Hour), 10)
	require.NoError(t, err)
	require.Len(t, due, 4)
	assertOutboundQueued(t, due, wecomResult.Outbound)
	assertOutboundQueued(t, due, telegramResult.Outbound)
	assertOutboundQueued(t, due, tenantBWeComResult.Outbound)
	assertOutboundQueued(t, due, groupResult.Outbound)
	records := audit.Records()
	require.Len(t, records, 4)
	for _, record := range records {
		expectedUserHash := platform.UserIDHash(record.TenantID, record.Channel, "user-shared")
		expectedInternalUserID := platform.InternalUserID(record.TenantID, record.Channel, "user-shared")
		assert.Equal(t, "completed", record.Decision)
		assert.NotEmpty(t, record.AuditID)
		assert.NotEmpty(t, record.SessionID)
		assert.Equal(t, expectedUserHash, record.UserID)
		assert.Equal(t, expectedUserHash, record.UserIDHash)
		assert.Equal(t, expectedInternalUserID, record.InternalUserID)
		assert.NotContains(t, record.UserID, "user-shared")
		assert.NotContains(t, record.UserIDHash, "user-shared")
		assert.NotContains(t, record.InternalUserID, "user-shared")
	}
	assert.Equal(t, platform.IdempotencyStatusCompleted, wecomResult.Status)
	assert.Equal(t, platform.IdempotencyStatusCompleted, telegramResult.Status)
	assert.Equal(t, platform.IdempotencyStatusCompleted, tenantBWeComResult.Status)
	assert.Equal(t, platform.IdempotencyStatusCompleted, groupResult.Status)
}

func TestServiceHandleInboundDuplicateReusesOutboxBackedResult(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "queued"}
	registerRuntime(t, registry, "tenant-a", r)
	outbox := channeladapter.NewInMemoryOutboxStore()
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewOutboxBackedOutboundStore(outbox),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")

	first, err := svc.HandleInbound(ctx, msg)
	require.NoError(t, err)
	second, err := svc.HandleInbound(ctx, msg)
	require.NoError(t, err)

	assert.True(t, second.Duplicate)
	assert.Equal(t, first.Outbound, second.Outbound)
	assert.Len(t, r.calls, 1)
	due, err := outbox.ListDue(ctx, time.Now().Add(time.Hour), 10)
	require.NoError(t, err)
	assert.Len(t, due, 1)
}

func TestServiceHandleInboundOutboxFailureDoesNotCompleteIdempotency(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "queued"}
	registerRuntime(t, registry, "tenant-a", r)
	idempotency := platform.NewInMemoryIdempotencyStore()
	outbox := channeladapter.NewInMemoryOutboxStore()
	store := NewOutboxBackedOutboundStore(outbox)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")
	resultRef := platform.IdempotencyKey("tenant-a", "wecom", "acct", "msg-1") + ":outbound:1"
	colliding := platform.OutboundMessage{
		TenantID:                 "other-tenant",
		BindingID:                "binding",
		Channel:                  "wecom",
		SessionID:                "session",
		ReplyToPlatformMessageID: "msg-1",
		Kind:                     platform.OutboundMessageKindText,
		Content:                  "already queued",
		Sequence:                 1,
		DedupKey:                 resultRef,
	}
	_, _, err := outbox.Enqueue(ctx, colliding, channeladapter.DefaultRetryPolicy())
	require.NoError(t, err)
	audit := platform.NewInMemoryAuditSink()
	svc := NewService(registry, idempotency, store, WithAuditSink(audit))

	_, err = svc.HandleInbound(ctx, msg)

	require.ErrorIs(t, err, channeladapter.ErrOutboundDuplicate)
	record, ok, err := idempotency.Get(ctx, platform.IdempotencyKey("tenant-a", "wecom", "acct", "msg-1"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, platform.IdempotencyStatusReplyFailed, record.Status)
	assert.Equal(t, resultRef, record.ResultRef)
	stored, ok, err := store.Get(ctx, resultRef)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "queued", stored.Content)
	require.Len(t, audit.Records(), 1)
	assert.NotEmpty(t, audit.Records()[0].AuditID)
	assert.Equal(t, "outbound_error", audit.Records()[0].Decision)
}

func TestServiceHandleInboundDuplicateReplyFailedReusesStoredOutbound(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "queued"}
	registerRuntime(t, registry, "tenant-a", r)
	idempotency := platform.NewInMemoryIdempotencyStore()
	outbox := channeladapter.NewInMemoryOutboxStore()
	store := NewOutboxBackedOutboundStore(outbox)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")
	resultRef := platform.IdempotencyKey("tenant-a", "wecom", "acct", "msg-1") + ":outbound:1"
	colliding := platform.OutboundMessage{
		TenantID:                 "other-tenant",
		BindingID:                "binding",
		Channel:                  "wecom",
		SessionID:                "session",
		ReplyToPlatformMessageID: "msg-1",
		Kind:                     platform.OutboundMessageKindText,
		Content:                  "already queued",
		Sequence:                 1,
		DedupKey:                 resultRef,
	}
	_, _, err := outbox.Enqueue(ctx, colliding, channeladapter.DefaultRetryPolicy())
	require.NoError(t, err)
	svc := NewService(registry, idempotency, store)
	_, err = svc.HandleInbound(ctx, msg)
	require.ErrorIs(t, err, channeladapter.ErrOutboundDuplicate)

	dup, err := svc.HandleInbound(ctx, msg)

	require.NoError(t, err)
	assert.True(t, dup.Duplicate)
	assert.False(t, dup.Processing)
	assert.Equal(t, platform.IdempotencyStatusReplyFailed, dup.Status)
	assert.Equal(t, resultRef, dup.ResultRef)
	assert.Equal(t, "queued", dup.Outbound.Content)
	assert.Len(t, r.calls, 1)
}

func TestServiceHandleInboundDuplicateProcessingDoesNotRun(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &blockingRunner{started: make(chan struct{})}
	registerRuntime(t, registry, "tenant-a", r)
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.HandleInbound(ctx, msg)
		errCh <- err
	}()
	<-r.started

	dup, err := svc.HandleInbound(ctx, msg)
	require.NoError(t, err)

	assert.True(t, dup.Duplicate)
	assert.True(t, dup.Processing)
	assert.Len(t, r.calls, 1)
	r.finish("done")
	require.NoError(t, <-errCh)
}

func TestServiceHandleInboundSerializesSameSession(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &blockingRunner{started: make(chan struct{})}
	registerRuntime(t, registry, "tenant-a", r)
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
	)
	first := inbound("tenant-a", "msg-1", "user-1", "hello")
	second := inbound("tenant-a", "msg-2", "user-1", "again")
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.HandleInbound(ctx, first)
		errCh <- err
	}()
	<-r.started

	busy, err := svc.HandleInbound(ctx, second)
	require.NoError(t, err)

	assert.False(t, busy.Duplicate)
	assert.True(t, busy.Processing)
	assert.Equal(t, platform.IdempotencyStatusProcessing, busy.Status)
	wantSessionID, err := platform.SessionIDForInbound(second)
	require.NoError(t, err)
	assert.Equal(t, wantSessionID, busy.SessionID)
	assert.Len(t, r.calls, 1)
	record, ok, err := svc.idempotencyStore.Get(
		ctx,
		platform.IdempotencyKey("tenant-a", "wecom", "acct", "msg-2"),
	)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, record.ResultRef)
	r.finish("done")
	require.NoError(t, <-errCh)

	r.finish("again")
	retry, err := svc.HandleInbound(ctx, second)
	require.NoError(t, err)
	assert.False(t, retry.Processing)
	assert.Equal(t, platform.IdempotencyStatusCompleted, retry.Status)
	assert.Len(t, r.calls, 2)
}

func TestServiceHandleInboundAllowsDifferentSessions(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "done"}
	registerRuntime(t, registry, "tenant-a", r)
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
	)

	_, err := svc.HandleInbound(ctx, inbound("tenant-a", "msg-1", "user-1", "hello"))
	require.NoError(t, err)
	_, err = svc.HandleInbound(ctx, inbound("tenant-a", "msg-2", "user-2", "hello"))
	require.NoError(t, err)

	require.Len(t, r.calls, 2)
	assert.NotEqual(t, r.calls[0].sessionID, r.calls[1].sessionID)
}

func TestServiceHandleInboundReleaseIgnoresCanceledRequestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	registry := NewInMemoryRegistry()
	runnerErr := errors.New("runner failed")
	r := &cancelingRunner{cancel: cancel, runErr: runnerErr}
	registerRuntime(t, registry, "tenant-a", r)
	lease := &recordingLease{}
	leaseStore := &recordingLeaseStore{lease: lease}
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
		WithSessionLeaseStore(leaseStore),
	)

	_, err := svc.HandleInbound(ctx, inbound("tenant-a", "msg-1", "user-1", "hello"))

	require.ErrorIs(t, err, runnerErr)
	require.True(t, lease.released)
	require.NoError(t, lease.ctxErr)
}

func TestServiceHandleInboundCancellationDuringEventCollectionReleasesSessionLease(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	registry := NewInMemoryRegistry()
	r := &hangingFirstRunner{
		started:  make(chan struct{}),
		response: "done",
	}
	registerRuntime(t, registry, "tenant-a", r)
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
	)
	first := inbound("tenant-a", "msg-1", "user-1", "hello")
	second := inbound("tenant-a", "msg-2", "user-1", "again")
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.HandleInbound(ctx, first)
		errCh <- err
	}()
	<-r.started

	cancel()
	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("HandleInbound did not return after context cancellation")
	}
	retry, err := svc.HandleInbound(context.Background(), second)

	require.NoError(t, err)
	assert.False(t, retry.Processing)
	assert.Equal(t, platform.IdempotencyStatusCompleted, retry.Status)
	assert.Len(t, r.calls, 2)
}

func TestServiceHandleInboundRejectsUnsupportedMessage(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "unused"}
	registerRuntime(t, registry, "tenant-a", r)
	audit := platform.NewInMemoryAuditSink()
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
		WithAuditSink(audit),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")
	msg.MessageType = platform.MessageTypeImage
	msg.ContentParts = []platform.ContentPart{{Type: platform.ContentPartTypeImage, FileRef: "artifact://image@1"}}

	_, err := svc.HandleInbound(ctx, msg)

	require.ErrorIs(t, err, ErrUnsupportedMessageType)
	assert.Empty(t, r.calls)
	require.Len(t, audit.Records(), 1)
	assert.NotEmpty(t, audit.Records()[0].AuditID)
	assert.Equal(t, "reject", audit.Records()[0].Decision)
	assert.NotEqual(t, "user-1", audit.Records()[0].UserID)
}

func TestServiceHandleInboundRejectsDisallowedUser(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "unused"}
	runtime := validRuntime("tenant-a", r)
	runtime.Binding.AllowedUsers = []string{"allowed-user"}
	require.NoError(t, registry.Register(runtime))
	audit := platform.NewInMemoryAuditSink()
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
		WithAuditSink(audit),
	)

	_, err := svc.HandleInbound(ctx, inbound("tenant-a", "msg-1", "blocked-user", "hello"))

	require.ErrorIs(t, err, ErrBindingAccessDenied)
	assert.Empty(t, r.calls)
	require.Len(t, audit.Records(), 1)
	assert.Equal(t, "reject", audit.Records()[0].Decision)
	assert.Equal(t, ErrBindingAccessDenied.Error(), audit.Records()[0].DecisionReason)
}

func TestServiceHandleInboundRejectsDisallowedGroup(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "unused"}
	runtime := validRuntime("tenant-a", r)
	runtime.Binding.AllowedGroups = []string{"allowed-group"}
	require.NoError(t, registry.Register(runtime))
	audit := platform.NewInMemoryAuditSink()
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
		WithAuditSink(audit),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")
	msg.ConversationType = platform.ConversationTypeGroup
	msg.ExternalGroupID = "blocked-group"

	_, err := svc.HandleInbound(ctx, msg)

	require.ErrorIs(t, err, ErrBindingAccessDenied)
	assert.Empty(t, r.calls)
	require.Len(t, audit.Records(), 1)
	assert.Equal(t, "reject", audit.Records()[0].Decision)
}

func TestServiceHandleInboundRejectsMissingRequiredMention(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "unused"}
	runtime := validRuntime("tenant-a", r)
	runtime.Binding.RequiredMention = true
	require.NoError(t, registry.Register(runtime))
	audit := platform.NewInMemoryAuditSink()
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
		WithAuditSink(audit),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")
	msg.ConversationType = platform.ConversationTypeGroup
	msg.ExternalGroupID = "group-1"
	msg.RequiredMentionSeen = false

	_, err := svc.HandleInbound(ctx, msg)

	require.ErrorIs(t, err, ErrBindingMentionRequired)
	assert.Empty(t, r.calls)
	require.Len(t, audit.Records(), 1)
	assert.Equal(t, "reject", audit.Records()[0].Decision)
	assert.Equal(t, ErrBindingMentionRequired.Error(), audit.Records()[0].DecisionReason)
}

func TestServiceHandleInboundAllowsAuthorizedGroupMention(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{response: "authorized"}
	runtime := validRuntime("tenant-a", r)
	runtime.Binding.AllowedUsers = []string{"user-1"}
	runtime.Binding.AllowedGroups = []string{"group-1"}
	runtime.Binding.RequiredMention = true
	require.NoError(t, registry.Register(runtime))
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")
	msg.ConversationType = platform.ConversationTypeGroup
	msg.ExternalGroupID = "group-1"
	msg.RequiredMentionSeen = true

	result, err := svc.HandleInbound(ctx, msg)

	require.NoError(t, err)
	assert.Equal(t, "authorized", result.Outbound.Content)
	require.Len(t, r.calls, 1)
}

func TestServiceHandleInboundRunnerErrorDoesNotComplete(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	runnerErr := errors.New("runner failed")
	r := &recordingRunner{runErr: runnerErr}
	registerRuntime(t, registry, "tenant-a", r)
	store := platform.NewInMemoryIdempotencyStore()
	svc := NewService(registry, store, NewInMemoryOutboundStore())
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")

	_, err := svc.HandleInbound(ctx, msg)

	require.ErrorIs(t, err, runnerErr)
	record, ok, err := store.Get(ctx, platform.IdempotencyKey("tenant-a", "wecom", "acct", "msg-1"))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, platform.IdempotencyStatusProcessing, record.Status)
	assert.Empty(t, record.ResultRef)
}

func TestServiceHandleInboundRunnerErrorRedactsAuditReason(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	runnerErr := errors.New("runner failed Authorization: Bearer raw-token api_key=sk-1234567890abcdef")
	r := &recordingRunner{runErr: runnerErr}
	registerRuntime(t, registry, "tenant-a", r)
	audit := platform.NewInMemoryAuditSink()
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
		WithAuditSink(audit),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")

	_, err := svc.HandleInbound(ctx, msg)

	require.ErrorIs(t, err, runnerErr)
	records := audit.Records()
	require.Len(t, records, 1)
	assert.Equal(t, "runner_error", records[0].Decision)
	assert.NotContains(t, records[0].DecisionReason, "raw-token")
	assert.NotContains(t, records[0].DecisionReason, "sk-1234567890abcdef")
	assert.Contains(t, records[0].DecisionReason, "Authorization: ****")
	assert.Contains(t, records[0].DecisionReason, "api_key=****")
}

func TestServiceHandleInboundUsesRequestIDAndStreamsText(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	r := &recordingRunner{chunks: []string{"hel", "lo"}}
	registerRuntime(t, registry, "tenant-a", r)
	audit := platform.NewInMemoryAuditSink()
	svc := NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		NewInMemoryOutboundStore(),
		WithAuditSink(audit),
	)
	msg := inbound("tenant-a", "msg-1", "user-1", "hello")
	msg.TraceContext = map[string]string{"request_id": "req-123"}

	result, err := svc.HandleInbound(ctx, msg)
	require.NoError(t, err)

	require.Len(t, r.calls, 1)
	assert.Equal(t, "req-123", r.calls[0].requestID)
	assert.Equal(t, "hello", result.Outbound.Content)
	assert.Equal(t, "req-123", result.Outbound.TraceID)
	require.Len(t, audit.Records(), 1)
	assert.Equal(t, "req-123", audit.Records()[0].RequestID)
	assert.Equal(t, "req-123", audit.Records()[0].TraceID)
}

func TestRuntimeValidateRejectsIdentifierMismatch(t *testing.T) {
	runtime := validRuntime("tenant-a", &recordingRunner{response: "unused"})
	runtime.Binding.TenantID = "tenant-b"

	err := runtime.Validate()

	require.ErrorIs(t, err, ErrRuntimeMismatch)
}

func TestServiceHandleInboundRejectsRegistryMismatch(t *testing.T) {
	ctx := context.Background()
	r := &recordingRunner{response: "unused"}
	registry := staticRegistry{runtime: Runtime{
		Tenant: platform.Tenant{
			TenantID: "tenant-b",
			Status:   platform.TenantStatusActive,
		},
		App: platform.AgentApp{
			TenantID: "tenant-b",
			AppID:    "app",
			AppName:  "app",
			Status:   platform.AppStatusActive,
		},
		Binding: platform.ChannelBinding{
			TenantID:    "tenant-b",
			AppID:       "app",
			BindingID:   "binding",
			Channel:     "wecom",
			AccountID:   "acct",
			WebhookPath: "/webhook",
			TokenRef:    "secret://token",
			SecretRef:   "secret://secret",
			Status:      platform.BindingStatusActive,
		},
		Runner: r,
	}}
	svc := NewService(registry, platform.NewInMemoryIdempotencyStore(), NewInMemoryOutboundStore())

	_, err := svc.HandleInbound(ctx, inbound("tenant-a", "msg-1", "user-1", "hello"))

	require.ErrorIs(t, err, ErrRuntimeMismatch)
	assert.Empty(t, r.calls)
}

func TestCollectAssistantTextStopsAtRunnerCompletion(t *testing.T) {
	ch := make(chan *event.Event, 2)
	ch <- responseEvent("done", true)
	ch <- event.NewResponseEvent(
		"invocation",
		"assistant",
		&model.Response{ID: "rc", Object: model.ObjectTypeRunnerCompletion, Done: true},
	)

	content, err := collectAssistantText(context.Background(), ch)

	require.NoError(t, err)
	assert.Equal(t, "done", content)
}

func TestCollectAssistantTextPrefersFinalFullMessage(t *testing.T) {
	ch := make(chan *event.Event, 3)
	ch <- chunkEvent("hel", true)
	ch <- chunkEvent("lo", true)
	ch <- responseEvent("hello", true)
	close(ch)

	content, err := collectAssistantText(context.Background(), ch)

	require.NoError(t, err)
	assert.Equal(t, "hello", content)
}

func registerRuntime(t *testing.T, registry *InMemoryRegistry, tenantID string, r runnerStub) {
	t.Helper()
	err := registry.Register(validRuntime(tenantID, r))
	require.NoError(t, err)
}

func validRuntime(tenantID string, r runnerStub) Runtime {
	return Runtime{
		Tenant: platform.Tenant{
			TenantID: tenantID,
			Status:   platform.TenantStatusActive,
		},
		App: platform.AgentApp{
			TenantID: tenantID,
			AppID:    "app",
			AppName:  "app",
			Status:   platform.AppStatusActive,
		},
		Binding: platform.ChannelBinding{
			TenantID:      tenantID,
			AppID:         "app",
			BindingID:     "binding",
			Channel:       "wecom",
			AccountID:     "acct",
			WebhookPath:   "/webhook",
			TokenRef:      "secret://token",
			SecretRef:     "secret://secret",
			Status:        platform.BindingStatusActive,
			ChannelLimits: platform.ChannelLimits{MaxTextLength: 4096},
		},
		Runner: r,
	}
}

func validRuntimeForBinding(
	tenantID string,
	appID string,
	bindingID string,
	channel string,
	accountID string,
	r runnerStub,
) Runtime {
	return Runtime{
		Tenant: platform.Tenant{
			TenantID: tenantID,
			Status:   platform.TenantStatusActive,
		},
		App: platform.AgentApp{
			TenantID: tenantID,
			AppID:    appID,
			AppName:  appID,
			Status:   platform.AppStatusActive,
		},
		Binding: platform.ChannelBinding{
			TenantID:      tenantID,
			AppID:         appID,
			BindingID:     bindingID,
			Channel:       channel,
			AccountID:     accountID,
			WebhookPath:   "/webhook/" + bindingID,
			TokenRef:      "secret://token/" + bindingID,
			SecretRef:     "secret://secret/" + bindingID,
			Status:        platform.BindingStatusActive,
			ChannelLimits: platform.ChannelLimits{MaxTextLength: 4096},
		},
		Runner: r,
	}
}

func inbound(tenantID, messageID, userID, text string) platform.InboundMessage {
	return platform.InboundMessage{
		TenantID:          tenantID,
		AppID:             "app",
		BindingID:         "binding",
		Channel:           "wecom",
		ChannelAccountID:  "acct",
		PlatformMessageID: messageID,
		ExternalUserID:    userID,
		ConversationType:  platform.ConversationTypeDM,
		MessageType:       platform.MessageTypeText,
		ContentParts: []platform.ContentPart{
			{Type: platform.ContentPartTypeText, Text: text},
		},
		ReceivedAt: time.Unix(100, 0),
	}
}

func inboundForRuntime(
	tenantID string,
	appID string,
	bindingID string,
	channel string,
	accountID string,
	messageID string,
	userID string,
	text string,
) platform.InboundMessage {
	return platform.InboundMessage{
		TenantID:          tenantID,
		AppID:             appID,
		BindingID:         bindingID,
		Channel:           channel,
		ChannelAccountID:  accountID,
		PlatformMessageID: messageID,
		ExternalUserID:    userID,
		ConversationType:  platform.ConversationTypeDM,
		MessageType:       platform.MessageTypeText,
		ContentParts: []platform.ContentPart{
			{Type: platform.ContentPartTypeText, Text: text},
		},
		ReceivedAt: time.Unix(100, 0),
	}
}

func assertOutboundQueued(
	t *testing.T,
	records []channeladapter.OutboxRecord,
	outbound platform.OutboundMessage,
) {
	t.Helper()
	for _, record := range records {
		if record.Message.DedupKey == outbound.DedupKey {
			assert.Equal(t, platform.OutboundStatusPending, record.Status)
			assert.Equal(t, outbound, record.Message)
			return
		}
	}
	t.Fatalf("outbound %q was not queued", outbound.DedupKey)
}

func assertRunnerCall(
	t *testing.T,
	call runnerCall,
	result Result,
	msg platform.InboundMessage,
	content string,
) {
	t.Helper()
	assert.Equal(t, result.SessionID, call.sessionID)
	assert.Equal(t, result.RequestID, call.requestID)
	assert.Equal(t, platform.InternalUserID(msg.TenantID, msg.Channel, msg.ExternalUserID), call.userID)
	assert.Equal(t, model.RoleUser, call.message.Role)
	assert.Equal(t, content, call.message.Content)
}

type runnerStub interface {
	Run(
		ctx context.Context,
		userID string,
		sessionID string,
		message model.Message,
		runOpts ...agent.RunOption,
	) (<-chan *event.Event, error)
	Close() error
}

type runnerCall struct {
	userID    string
	sessionID string
	message   model.Message
	requestID string
}

type recordingRunner struct {
	response string
	chunks   []string
	runErr   error
	calls    []runnerCall
}

type recordingOutboundProvider struct {
	status            platform.OutboundStatus
	providerMessageID string
	delivered         []platform.OutboundMessage
}

func (p *recordingOutboundProvider) Deliver(
	ctx context.Context,
	msg platform.OutboundMessage,
) (channeladapter.DeliveryResult, error) {
	if err := ctx.Err(); err != nil {
		return channeladapter.DeliveryResult{}, err
	}
	p.delivered = append(p.delivered, msg)
	return channeladapter.DeliveryResult{
		Status:            p.status,
		ProviderMessageID: p.providerMessageID,
	}, nil
}

func (r *recordingRunner) Run(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
	runOpts ...agent.RunOption,
) (<-chan *event.Event, error) {
	if r.runErr != nil {
		return nil, r.runErr
	}
	r.calls = append(r.calls, runnerCall{
		userID:    userID,
		sessionID: sessionID,
		message:   message,
		requestID: requestIDFromOptions(runOpts...),
	})
	out := make(chan *event.Event, 2)
	go func() {
		defer close(out)
		if len(r.chunks) > 0 {
			for i, chunk := range r.chunks {
				out <- chunkEvent(chunk, i != len(r.chunks)-1)
			}
			return
		}
		out <- responseEvent(r.response, true)
	}()
	return out, nil
}

func (r *recordingRunner) Close() error {
	return nil
}

type blockingRunner struct {
	mu          sync.Mutex
	started     chan struct{}
	startedOnce sync.Once
	done        chan string
	calls       []runnerCall
}

type cancelingRunner struct {
	cancel func()
	runErr error
	calls  []runnerCall
}

type staticRegistry struct {
	runtime Runtime
}

func (r staticRegistry) Lookup(
	ctx context.Context,
	msg platform.InboundMessage,
) (Runtime, bool, error) {
	if err := ctx.Err(); err != nil {
		return Runtime{}, false, err
	}
	return r.runtime, true, nil
}

func (r *blockingRunner) Run(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
	runOpts ...agent.RunOption,
) (<-chan *event.Event, error) {
	r.mu.Lock()
	if r.done == nil {
		r.done = make(chan string, 1)
	}
	r.calls = append(r.calls, runnerCall{
		userID:    userID,
		sessionID: sessionID,
		message:   message,
		requestID: requestIDFromOptions(runOpts...),
	})
	r.startedOnce.Do(func() {
		close(r.started)
	})
	done := r.done
	r.mu.Unlock()
	out := make(chan *event.Event, 1)
	go func() {
		defer close(out)
		select {
		case content := <-done:
			out <- responseEvent(content, true)
		case <-ctx.Done():
		}
	}()
	return out, nil
}

func (r *blockingRunner) Close() error {
	return nil
}

func (r *blockingRunner) finish(content string) {
	r.done <- content
}

func (r *cancelingRunner) Run(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
	runOpts ...agent.RunOption,
) (<-chan *event.Event, error) {
	r.calls = append(r.calls, runnerCall{
		userID:    userID,
		sessionID: sessionID,
		message:   message,
		requestID: requestIDFromOptions(runOpts...),
	})
	r.cancel()
	return nil, r.runErr
}

func (r *cancelingRunner) Close() error {
	return nil
}

type hangingFirstRunner struct {
	mu          sync.Mutex
	started     chan struct{}
	startedOnce sync.Once
	response    string
	calls       []runnerCall
}

func (r *hangingFirstRunner) Run(
	ctx context.Context,
	userID string,
	sessionID string,
	message model.Message,
	runOpts ...agent.RunOption,
) (<-chan *event.Event, error) {
	r.mu.Lock()
	callIndex := len(r.calls)
	r.calls = append(r.calls, runnerCall{
		userID:    userID,
		sessionID: sessionID,
		message:   message,
		requestID: requestIDFromOptions(runOpts...),
	})
	if callIndex == 0 {
		r.startedOnce.Do(func() {
			close(r.started)
		})
	}
	r.mu.Unlock()
	if callIndex == 0 {
		return make(chan *event.Event), nil
	}
	out := make(chan *event.Event, 1)
	out <- responseEvent(r.response, true)
	close(out)
	return out, nil
}

func (r *hangingFirstRunner) Close() error {
	return nil
}

type recordingLeaseStore struct {
	lease *recordingLease
}

func (s *recordingLeaseStore) Acquire(
	ctx context.Context,
	key SessionLeaseKey,
) (SessionLease, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	return s.lease, true, nil
}

type recordingLease struct {
	released bool
	ctxErr   error
}

func (l *recordingLease) Release(ctx context.Context) error {
	l.released = true
	l.ctxErr = ctx.Err()
	return nil
}

func responseEvent(content string, done bool) *event.Event {
	return event.NewResponseEvent(
		"invocation",
		"assistant",
		&model.Response{
			ID:     content,
			Object: model.ObjectTypeChatCompletion,
			Done:   done,
			Choices: []model.Choice{
				{Index: 0, Message: model.Message{Role: model.RoleAssistant, Content: content}},
			},
		},
	)
}

func chunkEvent(content string, partial bool) *event.Event {
	return event.NewResponseEvent(
		"invocation",
		"assistant",
		&model.Response{
			ID:        content,
			Object:    model.ObjectTypeChatCompletionChunk,
			Done:      !partial,
			IsPartial: partial,
			Choices: []model.Choice{
				{Index: 0, Delta: model.Message{Role: model.RoleAssistant, Content: content}},
			},
		},
	)
}

func requestIDFromOptions(opts ...agent.RunOption) string {
	var runOptions agent.RunOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&runOptions)
		}
	}
	return runOptions.RequestID
}
