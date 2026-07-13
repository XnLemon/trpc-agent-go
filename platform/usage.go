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

// UsageSink stores post-run usage records.
type UsageSink interface {
	// WriteUsage writes one usage record.
	WriteUsage(ctx context.Context, record UsageRecord) error
}

// InMemoryUsageSink is a concurrency-safe bounded usage sink for tests and demos.
type InMemoryUsageSink struct {
	records inMemoryRecords[UsageRecord]
}

// NewInMemoryUsageSink creates an in-memory usage sink.
func NewInMemoryUsageSink(options ...InMemorySinkOption) *InMemoryUsageSink {
	return &InMemoryUsageSink{
		records: newInMemoryRecords[UsageRecord](options...),
	}
}

// WriteUsage writes one usage record.
func (s *InMemoryUsageSink) WriteUsage(ctx context.Context, record UsageRecord) error {
	return s.records.append(ctx, record, UsageRecord.Validate)
}

// Records returns a snapshot of written usage records.
func (s *InMemoryUsageSink) Records() []UsageRecord {
	return s.records.snapshot()
}
