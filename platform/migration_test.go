//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import "testing"

func TestNormalizeStorageMigrationModeDefaultsEmptyToNormal(t *testing.T) {
	mode, err := NormalizeStorageMigrationMode("  ")
	if err != nil {
		t.Fatalf("normalize empty migration mode: %v", err)
	}
	if mode != StorageMigrationModeNormal {
		t.Fatalf("expected normal mode, got %q", mode)
	}
}

func TestNormalizeStorageMigrationModeAcceptsDocumentedModes(t *testing.T) {
	modes := []StorageMigrationMode{
		StorageMigrationModeNormal,
		StorageMigrationModeDualWrite,
		StorageMigrationModeShadowRead,
		StorageMigrationModeCutover,
		StorageMigrationModeRollback,
	}
	for _, want := range modes {
		t.Run(string(want), func(t *testing.T) {
			got, err := NormalizeStorageMigrationMode(" " + string(want) + " ")
			if err != nil {
				t.Fatalf("normalize migration mode: %v", err)
			}
			if got != want {
				t.Fatalf("expected %q, got %q", want, got)
			}
		})
	}
}

func TestNormalizeStorageMigrationModeRejectsUnknown(t *testing.T) {
	if _, err := NormalizeStorageMigrationMode("dual-read"); err == nil {
		t.Fatalf("expected unknown migration mode error")
	}
}

func TestStorageProfileValidateRejectsInvalidMigrationMode(t *testing.T) {
	profile := StorageProfile{
		TenantID:      "tenant",
		ProfileID:     "profile",
		Namespace:     "tenant/tenant",
		MigrationMode: "dual-read",
	}
	if err := profile.Validate(); err == nil {
		t.Fatalf("expected invalid migration mode error")
	}
}

func TestStorageProfileValidateRequiresTenantScopedNamespace(t *testing.T) {
	valid := StorageProfile{
		TenantID:  "tenant-a",
		ProfileID: "profile",
		Namespace: "tenant/tenant-a/profile/profile",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected tenant-scoped namespace to pass, got %v", err)
	}

	tests := []struct {
		name      string
		namespace string
	}{
		{name: "missing", namespace: ""},
		{name: "other_tenant", namespace: "tenant/tenant-b/profile/profile"},
		{name: "nested_other_tenant", namespace: "tenant/tenant-b/tenant-a/profile/profile"},
		{name: "shared", namespace: "shared/profile"},
		{name: "whitespace", namespace: " tenant/tenant-a/profile/profile "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := valid
			profile.Namespace = tt.namespace
			if err := profile.Validate(); err == nil {
				t.Fatalf("expected namespace %q to fail", tt.namespace)
			}
		})
	}
}

func TestIsActiveStorageMigrationMode(t *testing.T) {
	if IsActiveStorageMigrationMode(StorageMigrationModeNormal) {
		t.Fatalf("normal mode should not be active migration")
	}
	for _, mode := range []StorageMigrationMode{
		StorageMigrationModeDualWrite,
		StorageMigrationModeShadowRead,
		StorageMigrationModeCutover,
		StorageMigrationModeRollback,
	} {
		if !IsActiveStorageMigrationMode(mode) {
			t.Fatalf("%q should be active migration", mode)
		}
	}
}