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
	"testing"
)

func TestInMemorySessionLeaseStoreIssuesMonotonicFencingTokens(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionLeaseStore()
	key := SessionLeaseKey{TenantID: "tenant", AppID: "app", SessionID: "session"}

	first, acquired, err := store.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire first: %v", err)
	}
	if !acquired {
		t.Fatalf("first acquire should succeed")
	}
	firstFenced, ok := first.(SessionLeaseFencingToken)
	if !ok {
		t.Fatalf("first lease should expose fencing token capability")
	}
	if got := firstFenced.FencingToken(); got != 1 {
		t.Fatalf("expected first fencing token 1, got %d", got)
	}

	_, acquired, err = store.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire busy: %v", err)
	}
	if acquired {
		t.Fatalf("same session should not acquire while held")
	}
	if err := first.Release(ctx); err != nil {
		t.Fatalf("release first: %v", err)
	}

	second, acquired, err := store.Acquire(ctx, key)
	if err != nil {
		t.Fatalf("acquire second: %v", err)
	}
	if !acquired {
		t.Fatalf("second acquire should succeed after release")
	}
	secondFenced, ok := second.(SessionLeaseFencingToken)
	if !ok {
		t.Fatalf("second lease should expose fencing token capability")
	}
	if got := secondFenced.FencingToken(); got != 2 {
		t.Fatalf("expected second fencing token 2, got %d", got)
	}
}