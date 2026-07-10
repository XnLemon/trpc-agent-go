//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import "context"

type storageFencingTokenContextKey struct{}

// ContextWithStorageFencingToken returns a child context carrying a storage fencing token.
func ContextWithStorageFencingToken(ctx context.Context, token int64) context.Context {
	if token <= 0 {
		return ctx
	}
	return context.WithValue(ctx, storageFencingTokenContextKey{}, token)
}

// StorageFencingTokenFromContext returns the storage fencing token carried by ctx.
func StorageFencingTokenFromContext(ctx context.Context) (int64, bool) {
	token, ok := ctx.Value(storageFencingTokenContextKey{}).(int64)
	return token, ok && token > 0
}
