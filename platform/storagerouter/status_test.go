//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package storagerouter

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	artifactmemory "trpc.group/trpc-go/trpc-agent-go/artifact/inmemory"
	memoryinmemory "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func TestRouterStatusReportsAllResourcesReady(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	p := profile("tenant-a", "profile-a", "hot")
	p.MigrationMode = string(platform.StorageMigrationModeDualWrite)
	require.NoError(t, router.RegisterProfile(p))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "hot",
		Session:   sessioninmemory.NewSessionService(),
		Memory:    memoryinmemory.NewMemoryService(),
		Artifact:  artifactmemory.NewService(),
		Knowledge: &stubKnowledge{},
		Audit:     platform.NewInMemoryAuditSink(),
	}))

	summary, err := router.Status(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)

	assert.Equal(t, "tenant-a", summary.TenantID)
	assert.Equal(t, "profile-a", summary.ProfileID)
	assert.Equal(t, platform.StorageMigrationModeDualWrite, summary.MigrationMode)
	assert.True(t, summary.IsMigrating)
	assert.Equal(t, 5, summary.ReadyCount)
	assert.Equal(t, 0, summary.MissingCount)
	require.Len(t, summary.Resources, 5)
	for _, resource := range summary.Resources {
		assert.Equal(t, "hot", resource.BackendID)
		assert.Equal(t, ResourceStatusReady, resource.Status)
		assert.Empty(t, resource.Reason)
	}
}

func TestRouterStatusReportsMissingBackendAndService(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	p := profile("tenant-a", "profile-a", "hot")
	p.MemoryBackend = "missing"
	p.ArtifactBackend = ""
	require.NoError(t, router.RegisterProfile(p))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "hot",
		Session:   sessioninmemory.NewSessionService(),
		Audit:     platform.NewInMemoryAuditSink(),
	}))

	summary, err := router.Status(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)

	assert.False(t, summary.IsMigrating)
	assert.Equal(t, 2, summary.ReadyCount)
	assert.Equal(t, 3, summary.MissingCount)
	assertResourceStatus(t, summary, platform.BackendMigrationResourceSession, "hot", ResourceStatusReady)
	assertResourceStatus(t, summary, platform.BackendMigrationResourceMemory, "missing", ResourceStatusBackendMissing)
	assertResourceStatus(t, summary, platform.BackendMigrationResourceArtifact, "", ResourceStatusBackendMissing)
	assertResourceStatus(t, summary, platform.BackendMigrationResourceKnowledge, "hot", ResourceStatusServiceMissing)
	assertResourceStatus(t, summary, platform.BackendMigrationResourceAudit, "hot", ResourceStatusReady)
}

func TestRouterStatusRedactsUnsafeBackendIDs(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	p := profile("tenant-a", "profile-a", "hot")
	p.SessionBackend = "postgres://user:password@localhost/db"
	p.MemoryBackend = "sk-1234567890abcdef"
	p.ArtifactBackend = "safe.backend-1"
	require.NoError(t, router.RegisterProfile(p))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "safe.backend-1",
		Artifact:  artifactmemory.NewService(),
	}))

	summary, err := router.Status(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)

	assertResourceStatus(t, summary, platform.BackendMigrationResourceSession, "", ResourceStatusBackendMissing)
	assertResourceStatus(t, summary, platform.BackendMigrationResourceMemory, "", ResourceStatusBackendMissing)
	assertResourceStatus(t, summary, platform.BackendMigrationResourceArtifact, "safe.backend-1", ResourceStatusReady)
	for _, entry := range summary.Resources {
		assert.NotContains(t, entry.BackendID, "password")
		assert.NotContains(t, entry.Reason, "password")
		assert.NotContains(t, entry.BackendID, "sk-")
		assert.NotContains(t, entry.Reason, "sk-")
	}
}

func TestRouterStatusHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	router := NewInMemoryRouter()

	_, err := router.Status(ctx, "tenant-a", "profile-a")

	require.True(t, errors.Is(err, context.Canceled))
}

func TestRouterStatusReturnsContextCancellationAfterProfileLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	router := NewInMemoryRouter()
	require.NoError(t, router.RegisterProfile(profile("tenant-a", "profile-a", "hot")))
	cancel()

	_, err := router.Status(ctx, "tenant-a", "profile-a")

	require.True(t, errors.Is(err, context.Canceled))
}

func TestRouterStatusRejectsUnknownProfile(t *testing.T) {
	router := NewInMemoryRouter()

	_, err := router.Status(context.Background(), "tenant-a", "profile-a")

	require.ErrorIs(t, err, ErrProfileNotFound)
}

func assertResourceStatus(
	t *testing.T,
	summary StatusSummary,
	resource platform.BackendMigrationResource,
	backendID string,
	status ResourceStatus,
) {
	t.Helper()
	for _, entry := range summary.Resources {
		if entry.Resource == resource {
			assert.Equal(t, backendID, entry.BackendID)
			assert.Equal(t, status, entry.Status)
			if status == ResourceStatusReady {
				assert.Empty(t, entry.Reason)
			} else {
				assert.NotEmpty(t, entry.Reason)
			}
			return
		}
	}
	t.Fatalf("missing resource status for %q", resource)
}
