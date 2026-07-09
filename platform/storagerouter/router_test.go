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
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	memoryinmemory "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func TestRouterResolvesTenantStorageServices(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	sessionSvc := sessioninmemory.NewSessionService()
	memorySvc := memoryinmemory.NewMemoryService()
	artifactSvc := artifactmemory.NewService()
	knowledgeSvc := &stubKnowledge{}
	auditSink := platform.NewInMemoryAuditSink()
	require.NoError(t, router.RegisterProfile(profile("tenant-a", "profile-a", "hot")))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "hot",
		Session:   sessionSvc,
		Memory:    memorySvc,
		Artifact:  artifactSvc,
		Knowledge: knowledgeSvc,
		Audit:     auditSink,
	}))

	gotSession, err := router.Session(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)
	gotMemory, err := router.Memory(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)
	gotArtifact, err := router.Artifact(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)
	gotKnowledge, err := router.Knowledge(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)
	gotAudit, err := router.Audit(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)

	assert.Same(t, sessionSvc, gotSession)
	assert.Same(t, memorySvc, gotMemory)
	assert.Same(t, artifactSvc, gotArtifact)
	assert.Same(t, knowledgeSvc, gotKnowledge)
	assert.Same(t, auditSink, gotAudit)
}

func TestRouterRejectsCrossTenantLookup(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	require.NoError(t, router.RegisterProfile(profile("tenant-a", "profile-a", "hot")))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "hot",
		Session:   sessioninmemory.NewSessionService(),
	}))

	_, err := router.Session(ctx, "tenant-b", "profile-a")

	require.ErrorIs(t, err, ErrProfileNotFound)
}

func TestRouterRejectsMissingBackend(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	require.NoError(t, router.RegisterProfile(profile("tenant-a", "profile-a", "missing")))

	_, err := router.Session(ctx, "tenant-a", "profile-a")

	require.ErrorIs(t, err, ErrBackendNotFound)
}

func TestRouterRejectsMissingResourceService(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	require.NoError(t, router.RegisterProfile(profile("tenant-a", "profile-a", "hot")))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "hot",
	}))

	_, err := router.Memory(ctx, "tenant-a", "profile-a")

	require.ErrorIs(t, err, ErrBackendNotFound)
}

func TestRegisterProfileValidatesSecretRefs(t *testing.T) {
	router := NewInMemoryRouter()
	p := profile("tenant-a", "profile-a", "hot")
	p.DSNRef = "postgres://user:password@localhost/db"

	err := router.RegisterProfile(p)

	require.Error(t, err)
	assert.Contains(t, err.Error(), platform.ErrInlineSecretRejected.Error())
}

func TestRouterHonorsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	router := NewInMemoryRouter()

	_, err := router.Profile(ctx, "tenant-a", "profile-a")

	require.True(t, errors.Is(err, context.Canceled))
}

func profile(tenantID string, profileID string, backendID string) platform.StorageProfile {
	return platform.StorageProfile{
		TenantID:         tenantID,
		ProfileID:        profileID,
		SessionBackend:   backendID,
		MemoryBackend:    backendID,
		ArtifactBackend:  backendID,
		KnowledgeBackend: backendID,
		AuditBackend:     backendID,
		DSNRef:           "secret://storage",
		Namespace:        "tenant/" + tenantID,
	}
}

type stubKnowledge struct{}

func (s *stubKnowledge) Search(
	ctx context.Context,
	req *knowledge.SearchRequest,
) (*knowledge.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &knowledge.SearchResult{}, nil
}
