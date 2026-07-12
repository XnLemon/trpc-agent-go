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
)

// SessionLeaseStore serializes gateway execution for the same tenant/app/session.
type SessionLeaseStore interface {
	Acquire(ctx context.Context, key SessionLeaseKey) (SessionLease, bool, error)
}

// SessionLeaseKey identifies the gateway execution slot for one session.
type SessionLeaseKey struct {
	TenantID  string
	AppID     string
	SessionID string
}

// SessionLease releases one acquired session execution slot.
type SessionLease interface {
	Release(ctx context.Context) error
}

// SessionLeaseFencingToken is an optional lease capability that provides a
// storage fencing token for backends that enforce monotonic writes.
type SessionLeaseFencingToken interface {
	FencingToken() int64
}

// InMemorySessionLeaseStore is a process-local lease store for tests and demos.
type InMemorySessionLeaseStore struct {
	mu      sync.Mutex
	leases  map[SessionLeaseKey]int64
	counter map[SessionLeaseKey]int64
}

// NewInMemorySessionLeaseStore creates an empty process-local session lease store.
func NewInMemorySessionLeaseStore() *InMemorySessionLeaseStore {
	return &InMemorySessionLeaseStore{
		leases:  make(map[SessionLeaseKey]int64),
		counter: make(map[SessionLeaseKey]int64),
	}
}

// Acquire tries to acquire the session lease without waiting.
func (s *InMemorySessionLeaseStore) Acquire(
	ctx context.Context,
	key SessionLeaseKey,
) (SessionLease, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.leases[key]; ok {
		return nil, false, nil
	}
	token := s.counter[key] + 1
	s.counter[key] = token
	s.leases[key] = token
	return &inMemorySessionLease{
		store:        s,
		key:          key,
		fencingToken: token,
	}, true, nil
}

type inMemorySessionLease struct {
	store        *InMemorySessionLeaseStore
	key          SessionLeaseKey
	fencingToken int64
	once         sync.Once
}

func (l *inMemorySessionLease) FencingToken() int64 {
	if l == nil {
		return 0
	}
	return l.fencingToken
}

func (l *inMemorySessionLease) Release(ctx context.Context) error {
	l.once.Do(func() {
		l.store.mu.Lock()
		defer l.store.mu.Unlock()
		delete(l.store.leases, l.key)
	})
	return ctx.Err()
}
