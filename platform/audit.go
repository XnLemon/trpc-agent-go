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

// AuditSink stores audit records.
type AuditSink interface {
	// WriteAudit writes one audit record.
	WriteAudit(ctx context.Context, record AuditRecord) error
}

// InMemoryAuditSink is a concurrency-safe audit sink for tests and demos.
type InMemoryAuditSink struct {
	records inMemoryRecords[AuditRecord]
}

// NewInMemoryAuditSink creates an in-memory audit sink.
func NewInMemoryAuditSink() *InMemoryAuditSink {
	return &InMemoryAuditSink{}
}

// WriteAudit writes one audit record.
func (s *InMemoryAuditSink) WriteAudit(ctx context.Context, record AuditRecord) error {
	return s.records.append(ctx, record, AuditRecord.Validate)
}

// Records returns a snapshot of written audit records.
func (s *InMemoryAuditSink) Records() []AuditRecord {
	return s.records.snapshot()
}
