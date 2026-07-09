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
	"sync"
)

// MessageEventSink stores immutable conversation events.
type MessageEventSink interface {
	// WriteMessageEvent writes one message event.
	WriteMessageEvent(ctx context.Context, event MessageEvent) error
}

// InMemoryMessageEventSink is a concurrency-safe message event sink for tests and demos.
type InMemoryMessageEventSink struct {
	mu     sync.Mutex
	events []MessageEvent
}

// NewInMemoryMessageEventSink creates an in-memory message event sink.
func NewInMemoryMessageEventSink() *InMemoryMessageEventSink {
	return &InMemoryMessageEventSink{}
}

// WriteMessageEvent writes one message event.
func (s *InMemoryMessageEventSink) WriteMessageEvent(ctx context.Context, event MessageEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := event.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

// Events returns a snapshot of written message events.
func (s *InMemoryMessageEventSink) Events() []MessageEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]MessageEvent, len(s.events))
	copy(out, s.events)
	return out
}
