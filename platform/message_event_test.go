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
	"strings"
	"testing"
	"time"
)

func TestMessageEventValidateAcceptsSafeRecord(t *testing.T) {
	event := validMessageEvent()
	event.ContentJSON = `{"text":"hello"}`
	event.MetadataJSON = `{"source":"gateway"}`

	if err := event.Validate(); err != nil {
		t.Fatalf("expected valid message event, got %v", err)
	}
}

func TestMessageEventValidateRequiresIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*MessageEvent)
		want   string
	}{
		{name: "tenant", mutate: func(e *MessageEvent) { e.TenantID = " " }, want: "tenant_id"},
		{name: "app", mutate: func(e *MessageEvent) { e.AppID = " " }, want: "app_id"},
		{name: "session", mutate: func(e *MessageEvent) { e.SessionID = " " }, want: "session_id"},
		{name: "event", mutate: func(e *MessageEvent) { e.EventID = " " }, want: "event_id"},
		{name: "idempotency", mutate: func(e *MessageEvent) { e.IdempotencyKey = " " }, want: "idempotency_key"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := validMessageEvent()
			tt.mutate(&event)
			if err := event.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %s validation, got %v", tt.want, err)
			}
		})
	}
}

func TestMessageEventValidateRejectsInvalidRoleTypeAndSequence(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*MessageEvent)
		want   string
	}{
		{name: "role", mutate: func(e *MessageEvent) { e.Role = "admin" }, want: "role"},
		{name: "type", mutate: func(e *MessageEvent) { e.EventType = "secret" }, want: "event_type"},
		{name: "sequence", mutate: func(e *MessageEvent) { e.Sequence = 0 }, want: "sequence"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := validMessageEvent()
			tt.mutate(&event)
			if err := event.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %s validation, got %v", tt.want, err)
			}
		})
	}
}

func TestMessageEventValidateRejectsSensitiveTraceFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*MessageEvent)
		want   string
	}{
		{name: "app", mutate: func(e *MessageEvent) { e.AppID = "api_key=sk-1234567890abcdef" }, want: "app_id"},
		{name: "session", mutate: func(e *MessageEvent) { e.SessionID = "Authorization: Bearer raw-token" }, want: "session_id"},
		{name: "event", mutate: func(e *MessageEvent) { e.EventID = "password=plain" }, want: "event_id"},
		{name: "idempotency", mutate: func(e *MessageEvent) { e.IdempotencyKey = "token=raw-token" }, want: "idempotency_key"},
		{name: "trace", mutate: func(e *MessageEvent) { e.TraceID = "Authorization: Bearer raw-token" }, want: "trace_id"},
		{name: "content", mutate: func(e *MessageEvent) { e.ContentJSON = `{"api_key":"sk-1234567890abcdef"}` }, want: "content_json"},
		{name: "tool_calls", mutate: func(e *MessageEvent) { e.ToolCallsJSON = `{"password":"plain"}` }, want: "tool_calls_json"},
		{name: "metadata", mutate: func(e *MessageEvent) { e.MetadataJSON = `{"token":"raw-token"}` }, want: "metadata_json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := validMessageEvent()
			tt.mutate(&event)
			if err := event.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected %s validation, got %v", tt.want, err)
			}
		})
	}
}

func TestMessageEventSinkStoresSnapshot(t *testing.T) {
	sink := NewInMemoryMessageEventSink()
	event := validMessageEvent()

	if err := sink.WriteMessageEvent(context.Background(), event); err != nil {
		t.Fatalf("WriteMessageEvent: %v", err)
	}
	events := sink.Events()
	if len(events) != 1 {
		t.Fatalf("expected one message event, got %d", len(events))
	}
	events[0].TenantID = "changed"
	if sink.Events()[0].TenantID != "tenant" {
		t.Fatalf("Events should return a defensive copy")
	}
}

func TestMessageEventSinkRejectsInvalidRecord(t *testing.T) {
	sink := NewInMemoryMessageEventSink()
	event := validMessageEvent()
	event.TraceID = "password=plain"

	err := sink.WriteMessageEvent(context.Background(), event)
	if err == nil || !strings.Contains(err.Error(), "trace_id") {
		t.Fatalf("expected trace validation, got %v", err)
	}
	if got := sink.Events(); len(got) != 0 {
		t.Fatalf("expected invalid event to be rejected, got %+v", got)
	}
}

func validMessageEvent() MessageEvent {
	return MessageEvent{
		TenantID:       "tenant",
		AppID:          "app",
		SessionID:      "session",
		EventID:        "event",
		Sequence:       1,
		IdempotencyKey: "idempotency",
		Role:           MessageEventRoleUser,
		EventType:      MessageEventTypeMessage,
		TraceID:        "trace",
		CreatedAt:      time.Now(),
	}
}
