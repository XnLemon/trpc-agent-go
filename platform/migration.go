//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"fmt"
	"strings"
)

// StorageMigrationMode describes a storage backend migration phase.
type StorageMigrationMode string

const (
	// StorageMigrationModeNormal means no active migration is in progress.
	StorageMigrationModeNormal StorageMigrationMode = "normal"
	// StorageMigrationModeDualWrite writes new data to old and new backends.
	StorageMigrationModeDualWrite StorageMigrationMode = "dual_write"
	// StorageMigrationModeShadowRead compares old and new backend reads.
	StorageMigrationModeShadowRead StorageMigrationMode = "shadow_read"
	// StorageMigrationModeCutover routes reads and writes to the new backend.
	StorageMigrationModeCutover StorageMigrationMode = "cutover"
	// StorageMigrationModeRollback routes traffic back to the previous backend.
	StorageMigrationModeRollback StorageMigrationMode = "rollback"
)

// NormalizeStorageMigrationMode returns the canonical migration mode.
func NormalizeStorageMigrationMode(mode string) (StorageMigrationMode, error) {
	normalized := StorageMigrationMode(strings.TrimSpace(mode))
	if normalized == "" {
		return StorageMigrationModeNormal, nil
	}
	switch normalized {
	case StorageMigrationModeNormal,
		StorageMigrationModeDualWrite,
		StorageMigrationModeShadowRead,
		StorageMigrationModeCutover,
		StorageMigrationModeRollback:
		return normalized, nil
	default:
		return "", fmt.Errorf("invalid migration_mode %q", mode)
	}
}

// IsActiveStorageMigrationMode reports whether a mode represents active migration work.
func IsActiveStorageMigrationMode(mode StorageMigrationMode) bool {
	switch mode {
	case StorageMigrationModeDualWrite,
		StorageMigrationModeShadowRead,
		StorageMigrationModeCutover,
		StorageMigrationModeRollback:
		return true
	default:
		return false
	}
}
