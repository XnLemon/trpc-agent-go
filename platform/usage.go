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

// UsageSink stores post-run usage records.
type UsageSink interface {
	// WriteUsage writes one usage record.
	WriteUsage(ctx context.Context, record UsageRecord) error
}

// InMemoryUsageSink is a concurrency-safe usage sink for tests and demos.
type InMemoryUsageSink struct {
	mu      sync.Mutex
	records []UsageRecord
}

// NewInMemoryUsageSink creates an in-memory usage sink.
func NewInMemoryUsageSink() *InMemoryUsageSink {
	return &InMemoryUsageSink{}
}

// WriteUsage writes one usage record.
func (s *InMemoryUsageSink) WriteUsage(ctx context.Context, record UsageRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.records = append(s.records, record)
	return nil
}

// Records returns a snapshot of written usage records.
func (s *InMemoryUsageSink) Records() []UsageRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]UsageRecord, len(s.records))
	copy(out, s.records)
	return out
}
