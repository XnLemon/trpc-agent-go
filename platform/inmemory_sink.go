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
	"fmt"
	"sync"
)

const defaultInMemoryRecordLimit = 1024

type inMemoryRecordOptions struct {
	maxRecords int
}

// InMemorySinkOption configures in-memory platform sinks.
type InMemorySinkOption func(*inMemoryRecordOptions)

// WithInMemorySinkMaxRecords sets how many recent records an in-memory sink
// retains. The default is bounded to prevent unbounded demo/dev growth.
func WithInMemorySinkMaxRecords(maxRecords int) InMemorySinkOption {
	return func(opts *inMemoryRecordOptions) {
		opts.maxRecords = maxRecords
	}
}

type inMemoryRecords[T any] struct {
	mu         sync.Mutex
	records    []T
	maxRecords int
}

func newInMemoryRecords[T any](options ...InMemorySinkOption) inMemoryRecords[T] {
	opts := inMemoryRecordOptions{maxRecords: defaultInMemoryRecordLimit}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}
	return inMemoryRecords[T]{maxRecords: opts.maxRecords}
}

func (s *inMemoryRecords[T]) append(ctx context.Context, record T, validate func(T) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if validate != nil {
		if err := validate(record); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	maxRecords := s.maxRecords
	if maxRecords == 0 {
		maxRecords = defaultInMemoryRecordLimit
	}
	if maxRecords < 0 {
		return fmt.Errorf("in-memory sink max records must be positive")
	}
	if len(s.records) >= maxRecords {
		copy(s.records, s.records[1:])
		s.records = s.records[:maxRecords-1]
	}
	s.records = append(s.records, record)
	return nil
}

func (s *inMemoryRecords[T]) snapshot() []T {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]T, len(s.records))
	copy(out, s.records)
	return out
}
