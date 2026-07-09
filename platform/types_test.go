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
	"errors"
	"strings"
	"testing"
)

func TestSessionIDForInboundIsTenantScoped(t *testing.T) {
	base := InboundMessage{
		AppID:             "support",
		BindingID:         "telegram-bot-1",
		Channel:           "telegram",
		ChannelAccountID:  "bot-1",
		PlatformMessageID: "msg-1",
		ExternalUserID:    "same-user",
		ConversationType:  ConversationTypeDM,
		MessageType:       MessageTypeText,
	}
	a := base
	a.TenantID = "tenant-a"
	b := base
	b.TenantID = "tenant-b"

	sessionA, err := SessionIDForInbound(a)
	if err != nil {
		t.Fatalf("SessionIDForInbound tenant-a: %v", err)
	}
	sessionB, err := SessionIDForInbound(b)
	if err != nil {
		t.Fatalf("SessionIDForInbound tenant-b: %v", err)
	}
	if sessionA == sessionB {
		t.Fatalf("sessions should differ across tenants: %q", sessionA)
	}
	if !strings.HasPrefix(sessionA, "ses_") || strings.Contains(sessionA, ":") {
		t.Fatalf("session id should be opaque and delimiter-safe, got %q", sessionA)
	}
}

func TestSessionIDForInboundSupportsGroupAndThread(t *testing.T) {
	groupID, err := SessionID("tenant", "app", "wecom", ConversationTypeGroup, "user", "room 1", "")
	if err != nil {
		t.Fatalf("group session: %v", err)
	}
	groupIDAgain, err := SessionID("tenant", "app", "wecom", ConversationTypeGroup, "user", "room 1", "")
	if err != nil {
		t.Fatalf("group session again: %v", err)
	}
	if groupID != groupIDAgain {
		t.Fatalf("expected stable group id, got %q and %q", groupID, groupIDAgain)
	}
	if !strings.HasPrefix(groupID, "ses_") || strings.Contains(groupID, ":") {
		t.Fatalf("group session id should be opaque and delimiter-safe, got %q", groupID)
	}

	threadID, err := SessionID("tenant", "app", "telegram", ConversationTypeThread, "user", "chat", "topic/7")
	if err != nil {
		t.Fatalf("thread session: %v", err)
	}
	if !strings.HasPrefix(threadID, "ses_") || strings.Contains(threadID, ":") {
		t.Fatalf("thread session id should be opaque and delimiter-safe, got %q", threadID)
	}
	if groupID == threadID {
		t.Fatalf("group and thread sessions should differ: %q", groupID)
	}
}

func TestStableIDsAreDelimiterSafe(t *testing.T) {
	sessionA, err := SessionID("tenant", "app", "chat:dm:user", ConversationTypeDM, "leaf", "", "")
	if err != nil {
		t.Fatalf("session A: %v", err)
	}
	sessionB, err := SessionID("tenant", "app", "chat", ConversationTypeDM, "user:dm:leaf", "", "")
	if err != nil {
		t.Fatalf("session B: %v", err)
	}
	if sessionA == sessionB {
		t.Fatalf("delimiter-bearing session parts should not collide: %q", sessionA)
	}

	keyA := IdempotencyKey("tenant", "chat:account:bot", "primary", "msg")
	keyB := IdempotencyKey("tenant", "chat", "bot:account:primary", "msg")
	if keyA == keyB {
		t.Fatalf("delimiter-bearing idempotency parts should not collide: %q", keyA)
	}
	for _, key := range []string{keyA, keyB} {
		if !strings.HasPrefix(key, "idem_") || strings.Contains(key, ":") {
			t.Fatalf("idempotency key should be opaque and delimiter-safe, got %q", key)
		}
	}
}

func TestValidateInboundRequiresGroupForGroupConversation(t *testing.T) {
	msg := InboundMessage{
		TenantID:          "tenant",
		AppID:             "app",
		BindingID:         "binding",
		Channel:           "telegram",
		ChannelAccountID:  "bot",
		PlatformMessageID: "msg",
		ExternalUserID:    "user",
		ConversationType:  ConversationTypeGroup,
	}
	if err := msg.Validate(); !errors.Is(err, ErrExternalGroupIDRequired) {
		t.Fatalf("expected ErrExternalGroupIDRequired, got %v", err)
	}
}

func TestValidateInboundRequiresBindingID(t *testing.T) {
	msg := InboundMessage{
		TenantID:          "tenant",
		AppID:             "app",
		Channel:           "telegram",
		ChannelAccountID:  "bot",
		PlatformMessageID: "msg",
		ExternalUserID:    "user",
		ConversationType:  ConversationTypeDM,
		MessageType:       MessageTypeText,
	}
	if err := msg.Validate(); !errors.Is(err, ErrBindingIDRequired) {
		t.Fatalf("expected ErrBindingIDRequired, got %v", err)
	}
}

func TestValidateInboundEventDoesNotRequireConversationIdentity(t *testing.T) {
	msg := InboundMessage{
		TenantID:          "tenant",
		AppID:             "app",
		BindingID:         "binding",
		Channel:           "telegram",
		ChannelAccountID:  "bot",
		PlatformMessageID: "event-1",
		MessageType:       MessageTypeEvent,
		RawEventType:      "app_mention",
	}
	if err := msg.Validate(); err != nil {
		t.Fatalf("event should not require user or conversation identity, got %v", err)
	}
}

func TestSessionIDForInboundIncludesBindingAndAccountScope(t *testing.T) {
	base := InboundMessage{
		TenantID:          "tenant",
		AppID:             "app",
		BindingID:         "binding-a",
		Channel:           "telegram",
		ChannelAccountID:  "bot-a",
		PlatformMessageID: "msg",
		ExternalUserID:    "same-user",
		ConversationType:  ConversationTypeDM,
		MessageType:       MessageTypeText,
	}
	bindingA, err := SessionIDForInbound(base)
	if err != nil {
		t.Fatalf("binding A: %v", err)
	}

	bindingVariant := base
	bindingVariant.BindingID = "binding-b"
	bindingB, err := SessionIDForInbound(bindingVariant)
	if err != nil {
		t.Fatalf("binding B: %v", err)
	}
	if bindingA == bindingB {
		t.Fatalf("sessions should differ across bindings: %q", bindingA)
	}

	accountVariant := base
	accountVariant.ChannelAccountID = "bot-b"
	accountB, err := SessionIDForInbound(accountVariant)
	if err != nil {
		t.Fatalf("account B: %v", err)
	}
	if bindingA == accountB {
		t.Fatalf("sessions should differ across channel accounts: %q", bindingA)
	}
}

func TestInternalUserIDIsStableAndTenantScoped(t *testing.T) {
	a1 := InternalUserID("tenant-a", "telegram", "42")
	a2 := InternalUserID("tenant-a", "telegram", "42")
	b := InternalUserID("tenant-b", "telegram", "42")
	if a1 != a2 {
		t.Fatalf("expected stable id, got %q and %q", a1, a2)
	}
	if a1 == b {
		t.Fatalf("expected tenant scoped ids, got %q", a1)
	}
}

func TestAuditIDIsStableAndScoped(t *testing.T) {
	a1 := AuditID("tenant-a", "app", "trace", "decision")
	a2 := AuditID("tenant-a", "app", "trace", "decision")
	b := AuditID("tenant-b", "app", "trace", "decision")
	if a1 != a2 {
		t.Fatalf("expected stable audit id, got %q and %q", a1, a2)
	}
	if a1 == b {
		t.Fatalf("expected scoped audit ids, got %q", a1)
	}
	if !strings.HasPrefix(a1, "audit_") {
		t.Fatalf("expected audit id prefix, got %q", a1)
	}
}

func TestIdempotencyStoreDoesNotRestartCompletedMessage(t *testing.T) {
	store := NewInMemoryIdempotencyStore()
	record := IdempotencyRecord{
		TenantID:          "tenant",
		Channel:           "telegram",
		AccountID:         "bot",
		PlatformMessageID: "msg",
		RequestID:         "req-1",
		SessionID:         "session",
	}
	first, started, err := store.Start(context.Background(), record)
	if err != nil {
		t.Fatalf("start first: %v", err)
	}
	if !started {
		t.Fatalf("first start should create the record")
	}
	completed, err := store.Complete(context.Background(), first.IdempotencyKey, "outbound-1")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Status != IdempotencyStatusCompleted {
		t.Fatalf("expected completed status, got %q", completed.Status)
	}

	again, started, err := store.Start(context.Background(), record)
	if err != nil {
		t.Fatalf("start duplicate: %v", err)
	}
	if started {
		t.Fatalf("duplicate message should not start runner again")
	}
	if again.Status != IdempotencyStatusCompleted || again.ResultRef != "outbound-1" {
		t.Fatalf("duplicate should return completed result, got %#v", again)
	}
}

func TestIdempotencyStartRejectsMissingKeyFields(t *testing.T) {
	tests := []struct {
		name   string
		record IdempotencyRecord
		want   error
	}{
		{
			name: "tenant",
			record: IdempotencyRecord{
				Channel:           "telegram",
				AccountID:         "bot",
				PlatformMessageID: "msg",
			},
			want: ErrTenantIDRequired,
		},
		{
			name: "channel",
			record: IdempotencyRecord{
				TenantID:          "tenant",
				AccountID:         "bot",
				PlatformMessageID: "msg",
			},
			want: ErrChannelRequired,
		},
		{
			name: "account",
			record: IdempotencyRecord{
				TenantID:          "tenant",
				Channel:           "telegram",
				PlatformMessageID: "msg",
			},
			want: ErrAccountIDRequired,
		},
		{
			name: "message",
			record: IdempotencyRecord{
				TenantID:  "tenant",
				Channel:   "telegram",
				AccountID: "bot",
			},
			want: ErrPlatformMessageIDRequired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryIdempotencyStore()
			_, started, err := store.Start(context.Background(), tt.record)
			if !errors.Is(err, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, err)
			}
			if started {
				t.Fatalf("invalid record should not start")
			}
		})
	}
}

func TestIdempotencyStartRejectsMismatchedCallerKey(t *testing.T) {
	store := NewInMemoryIdempotencyStore()
	record := IdempotencyRecord{
		TenantID:          "tenant",
		Channel:           "telegram",
		AccountID:         "bot-a",
		PlatformMessageID: "msg",
		IdempotencyKey:    IdempotencyKey("tenant", "telegram", "bot-b", "msg"),
	}
	_, started, err := store.Start(context.Background(), record)
	if err == nil {
		t.Fatalf("expected mismatched caller-supplied idempotency key to fail")
	}
	if started {
		t.Fatalf("mismatched caller-supplied key should not start")
	}
}

func TestIdempotencyStoreEnforcesStateTransitions(t *testing.T) {
	ctx := context.Background()
	processingStore := NewInMemoryIdempotencyStore()
	processing, started, err := processingStore.Start(ctx, IdempotencyRecord{
		TenantID:          "tenant",
		Channel:           "telegram",
		AccountID:         "bot",
		PlatformMessageID: "msg-processing",
	})
	if err != nil {
		t.Fatalf("start processing: %v", err)
	}
	if !started {
		t.Fatalf("processing record should start")
	}
	if _, err := processingStore.MarkReplyFailed(ctx, processing.IdempotencyKey, "outbound-1"); err == nil {
		t.Fatalf("reply failure should only be allowed after completion")
	}

	completedStore := NewInMemoryIdempotencyStore()
	completed, started, err := completedStore.Start(ctx, IdempotencyRecord{
		TenantID:          "tenant",
		Channel:           "telegram",
		AccountID:         "bot",
		PlatformMessageID: "msg-completed",
	})
	if err != nil {
		t.Fatalf("start completed: %v", err)
	}
	if !started {
		t.Fatalf("completed record should start")
	}
	if _, err := completedStore.Complete(ctx, completed.IdempotencyKey, "outbound-1"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := completedStore.Complete(ctx, completed.IdempotencyKey, "outbound-2"); err == nil {
		t.Fatalf("completed record should not be completed again")
	}
	if _, err := completedStore.MarkReplyFailed(ctx, completed.IdempotencyKey, "outbound-1"); err != nil {
		t.Fatalf("mark reply failed from completed: %v", err)
	}
	if _, err := completedStore.Complete(ctx, completed.IdempotencyKey, "outbound-3"); err == nil {
		t.Fatalf("reply-failed record should not transition back to completed")
	}
}

func TestBindingRejectsInlineSecrets(t *testing.T) {
	binding := ChannelBinding{
		TenantID:    "tenant",
		AppID:       "app",
		BindingID:   "binding",
		Channel:     "telegram",
		AccountID:   "bot",
		WebhookPath: "/channels/telegram/binding/callback",
		SecretRef:   "secret=plain",
	}
	if err := binding.Validate(); !errors.Is(err, ErrInlineSecretRejected) {
		t.Fatalf("expected inline secret rejection, got %v", err)
	}
}

func TestBindingRejectsOpaqueRawSecret(t *testing.T) {
	binding := ChannelBinding{
		TenantID:    "tenant",
		AppID:       "app",
		BindingID:   "binding",
		Channel:     "telegram",
		AccountID:   "bot",
		WebhookPath: "/channels/telegram/binding/callback",
		SecretRef:   "sk-1234567890abcdef",
	}
	if err := binding.Validate(); !errors.Is(err, ErrInlineSecretRejected) {
		t.Fatalf("expected inline secret rejection, got %v", err)
	}
}

func TestBindingRejectsTelegramBotToken(t *testing.T) {
	binding := ChannelBinding{
		TenantID:    "tenant",
		AppID:       "app",
		BindingID:   "binding",
		Channel:     "telegram",
		AccountID:   "bot",
		WebhookPath: "/channels/telegram/binding/callback",
		TokenRef:    "123456789:AAExampleRawTelegramToken",
	}
	if err := binding.Validate(); !errors.Is(err, ErrInlineSecretRejected) {
		t.Fatalf("expected inline secret rejection, got %v", err)
	}
}

func TestBindingRejectsURLUserinfoWithMultipleAtSigns(t *testing.T) {
	binding := ChannelBinding{
		TenantID:    "tenant",
		AppID:       "app",
		BindingID:   "binding",
		Channel:     "telegram",
		AccountID:   "bot",
		WebhookPath: "/channels/telegram/binding/callback",
		SecretRef:   "postgres://svc@example.com:password@db/prod",
	}
	if err := binding.Validate(); !errors.Is(err, ErrInlineSecretRejected) {
		t.Fatalf("expected inline URL credential rejection, got %v", err)
	}
}

func TestBindingAllowsURISecretReferences(t *testing.T) {
	binding := ChannelBinding{
		TenantID:    "tenant",
		AppID:       "app",
		BindingID:   "binding",
		Channel:     "telegram",
		AccountID:   "bot",
		WebhookPath: "/channels/telegram/binding/callback",
		SecretRef:   "kms://tenant/telegram-bot",
	}
	if err := binding.Validate(); err != nil {
		t.Fatalf("expected URI reference to be accepted, got %v", err)
	}
}

func TestModelProfileRejectsInlineSecrets(t *testing.T) {
	profile := ModelProfile{
		TenantID:  "tenant",
		ProfileID: "model",
		APIKeyRef: "sk-1234567890abcdef",
	}
	if err := profile.Validate(); !errors.Is(err, ErrInlineSecretRejected) {
		t.Fatalf("expected inline secret rejection, got %v", err)
	}
}

func TestIdempotencyUpdateUnknownKeyFails(t *testing.T) {
	store := NewInMemoryIdempotencyStore()
	_, err := store.Complete(context.Background(), "missing", "result")
	if !errors.Is(err, ErrIdempotencyRecordNotFound) {
		t.Fatalf("expected missing record error, got %v", err)
	}

	record := IdempotencyRecord{
		TenantID:          "tenant",
		Channel:           "telegram",
		AccountID:         "bot",
		PlatformMessageID: "missing",
	}
	_, started, err := store.Start(context.Background(), record)
	if err != nil {
		t.Fatalf("start after failed complete: %v", err)
	}
	if !started {
		t.Fatalf("failed complete must not poison future starts")
	}
}

func TestRedactorMasksSecrets(t *testing.T) {
	redactor, err := NewRedactor()
	if err != nil {
		t.Fatalf("NewRedactor: %v", err)
	}
	input := `api_key=sk-1234567890abcdef Authorization=Bearer token-value Authorization: Basic abc123 token: raw "password":"json-secret" db=postgres://u:pass@example/db`
	got := redactor.Redact(input)
	for _, leaked := range []string{
		"sk-1234567890abcdef",
		"token-value",
		"abc123",
		"raw",
		"json-secret",
		":pass@",
	} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted output leaked %q: %q", leaked, got)
		}
	}
	if !strings.Contains(got, "api_key=****") {
		t.Fatalf("expected api_key mask, got %q", got)
	}
}

func TestRedactorMasksNonBearerAuthorizationCredentials(t *testing.T) {
	redactor, err := NewRedactor()
	if err != nil {
		t.Fatalf("NewRedactor: %v", err)
	}
	input := "Authorization: Token top-secret\nAuthorization=Digest username=\"bob\", response=\"abc123\"\n"
	got := redactor.Redact(input)
	for _, leaked := range []string{"top-secret", "username=\"bob\"", "abc123"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted output leaked %q: %q", leaked, got)
		}
	}
	if !strings.Contains(got, "Authorization: ****") {
		t.Fatalf("expected header authorization mask, got %q", got)
	}
	if !strings.Contains(got, "Authorization=****") {
		t.Fatalf("expected key-value authorization mask, got %q", got)
	}
}

func TestRedactorMasksURLUserinfoWithMultipleAtSigns(t *testing.T) {
	redactor, err := NewRedactor()
	if err != nil {
		t.Fatalf("NewRedactor: %v", err)
	}
	input := "db=postgres://user:pa@ss@word@example.com/db"
	got := redactor.Redact(input)
	if strings.Contains(got, "pa@ss@word") ||
		strings.Contains(got, "ss@word@example.com") {
		t.Fatalf("redacted output leaked URL password fragments: %q", got)
	}
	if !strings.Contains(got, "postgres://user:****@example.com/db") {
		t.Fatalf("expected URL userinfo password mask, got %q", got)
	}
}

func TestAuditSinkStoresSnapshot(t *testing.T) {
	sink := NewInMemoryAuditSink()
	record := AuditRecord{
		TenantID:       "tenant",
		AuditID:        "audit",
		UserID:         "internal",
		InternalUserID: "usr",
		UserIDHash:     UserIDHash("tenant", "telegram", "external"),
		TraceID:        "trace",
	}
	if err := sink.WriteAudit(context.Background(), record); err != nil {
		t.Fatalf("WriteAudit: %v", err)
	}
	records := sink.Records()
	if len(records) != 1 {
		t.Fatalf("expected one audit record, got %d", len(records))
	}
	records[0].TenantID = "changed"
	if sink.Records()[0].TenantID != "tenant" {
		t.Fatalf("Records should return a defensive copy")
	}
}

func TestAuditSinkRejectsInvalidRecord(t *testing.T) {
	sink := NewInMemoryAuditSink()
	record := AuditRecord{
		TenantID: "tenant",
	}

	err := sink.WriteAudit(context.Background(), record)
	if err == nil || !strings.Contains(err.Error(), "audit_id is required") {
		t.Fatalf("expected audit_id validation, got %v", err)
	}
	if got := sink.Records(); len(got) != 0 {
		t.Fatalf("expected invalid record to be rejected, got %+v", got)
	}
}

func TestAuditSinkRejectsSensitiveRecord(t *testing.T) {
	sink := NewInMemoryAuditSink()
	record := AuditRecord{
		TenantID:       "tenant",
		AuditID:        "audit",
		UserID:         "internal",
		InternalUserID: "usr",
		UserIDHash:     UserIDHash("tenant", "telegram", "external"),
		TraceID:        "trace",
		DecisionReason: "Authorization: Bearer raw-token",
	}

	err := sink.WriteAudit(context.Background(), record)
	if err == nil || !strings.Contains(err.Error(), "decision_reason") {
		t.Fatalf("expected sensitive record validation, got %v", err)
	}
	if got := sink.Records(); len(got) != 0 {
		t.Fatalf("expected sensitive record to be rejected, got %+v", got)
	}
}
