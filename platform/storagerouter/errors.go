//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package storagerouter

import "errors"

var (
	// ErrProfileNotFound indicates that a storage profile is not registered.
	ErrProfileNotFound = errors.New("storage router profile not found")
	// ErrTenantMismatch indicates that a profile belongs to another tenant.
	ErrTenantMismatch = errors.New("storage router tenant mismatch")
	// ErrBackendNotFound indicates that a requested backend is not registered.
	ErrBackendNotFound = errors.New("storage router backend not found")
	// ErrBackendTenantMismatch indicates that a registered backend belongs to another tenant.
	ErrBackendTenantMismatch = errors.New("storage router backend tenant mismatch")
)
