//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

// Package platform contains reusable multi-tenant platform contracts.
//
// The package is intentionally small and dependency-light: it models tenant,
// channel, governance, idempotency, and audit data that gateway or channel
// adapter implementations can share without changing the core runner/session
// interfaces.
package platform
