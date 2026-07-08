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
	if !strings.Contains(sessionA, "tenant:tenant-a:app:support:channel:telegram:dm:same-user") {
		t.Fatalf("unexpected dm session id: %q", sessionA)
	}
}

func TestSessionIDForInboundSupportsGroupAndThread(t *testing.T) {
	groupID, err := SessionID("tenant", "app", "wecom", ConversationTypeGroup, "user", "room 1", "")
	if err != nil {
		t.Fatalf("group session: %v", err)
	}
	if groupID != "tenant:tenant:app:app:channel:wecom:group:room%201" {
		t.Fatalf("unexpected group id: %q", groupID)
	}

	threadID, err := SessionID("tenant", "app", "telegram", ConversationTypeThread, "user", "chat", "topic/7")
	if err != nil {
		t.Fatalf("thread session: %v", err)
	}
	if threadID != "tenant:tenant:app:app:channel:telegram:group:chat:thread:topic%2F7" {
		t.Fatalf("unexpected thread id: %q", threadID)
	}
}

func TestValidateInboundRequiresGroupForGroupConversation(t *testing.T) {
	msg := InboundMessage{
		TenantID:          "tenant",
		AppID:             "app",
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
