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
)

// MessageEventSink stores immutable conversation events.
type MessageEventSink interface {
	// WriteMessageEvent writes one message event.
	WriteMessageEvent(ctx context.Context, event MessageEvent) error
}

// InMemoryMessageEventSink is a concurrency-safe bounded message event sink for tests and demos.
type InMemoryMessageEventSink struct {
	events inMemoryRecords[MessageEvent]
}

// NewInMemoryMessageEventSink creates an in-memory message event sink.
func NewInMemoryMessageEventSink(options ...InMemorySinkOption) *InMemoryMessageEventSink {
	return &InMemoryMessageEventSink{
		events: newInMemoryRecords[MessageEvent](options...),
	}
}

// WriteMessageEvent writes one message event.
func (s *InMemoryMessageEventSink) WriteMessageEvent(ctx context.Context, event MessageEvent) error {
	return s.events.append(ctx, event, MessageEvent.Validate)
}

// Events returns a snapshot of written message events.
func (s *InMemoryMessageEventSink) Events() []MessageEvent {
	return s.events.snapshot()
}
