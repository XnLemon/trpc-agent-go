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
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/platform"
)

// OutboundStore stores gateway replies so duplicate callbacks can reuse completed results.
type OutboundStore interface {
	Save(ctx context.Context, resultRef string, outbound platform.OutboundMessage) error
	Get(ctx context.Context, resultRef string) (platform.OutboundMessage, bool, error)
}

// InMemoryOutboundStore is a concurrency-safe outbound store for tests and demos.
type InMemoryOutboundStore struct {
	mu       sync.Mutex
	messages map[string]platform.OutboundMessage
}

// NewInMemoryOutboundStore creates an in-memory outbound store.
func NewInMemoryOutboundStore() *InMemoryOutboundStore {
	return &InMemoryOutboundStore{
		messages: make(map[string]platform.OutboundMessage),
	}
}

// Save stores one outbound message under resultRef.
func (s *InMemoryOutboundStore) Save(
	ctx context.Context,
	resultRef string,
	outbound platform.OutboundMessage,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages[resultRef] = outbound
	return nil
}

// Get returns a stored outbound message.
func (s *InMemoryOutboundStore) Get(
	ctx context.Context,
	resultRef string,
) (platform.OutboundMessage, bool, error) {
	if err := ctx.Err(); err != nil {
		return platform.OutboundMessage{}, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	outbound, ok := s.messages[resultRef]
	return outbound, ok, nil
}
