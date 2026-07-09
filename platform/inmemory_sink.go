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

type inMemoryRecords[T any] struct {
	mu      sync.Mutex
	records []T
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
