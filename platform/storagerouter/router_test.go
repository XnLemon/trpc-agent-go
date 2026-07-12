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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	artifactmemory "trpc.group/trpc-go/trpc-agent-go/artifact/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	memoryinmemory "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

var _ Router = (*InMemoryRouter)(nil)

func TestRouterResolvesTenantStorageServices(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	sessionSvc := sessioninmemory.NewSessionService()
	summarySvc := sessioninmemory.NewSessionService()
	memorySvc := memoryinmemory.NewMemoryService()
	artifactSvc := artifactmemory.NewService()
	knowledgeSvc := &stubKnowledge{}
	auditSink := platform.NewInMemoryAuditSink()
	p := profile("tenant-a", "profile-a", "hot")
	p.SummaryBackend = "summary-hot"
	require.NoError(t, router.RegisterProfile(p))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "hot",
		Session:   sessionSvc,
		Memory:    memorySvc,
		Artifact:  artifactSvc,
		Knowledge: knowledgeSvc,
		Audit:     auditSink,
	}))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "summary-hot",
		Summary:   summarySvc,
	}))

	gotSession, err := router.Session(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)
	gotSummary, err := router.Summary(ctx, "tenant-a", "profile-a")
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
	assert.Same(t, summarySvc, gotSummary)
	assert.Same(t, memorySvc, gotMemory)
	assert.Same(t, artifactSvc, gotArtifact)
	assert.Same(t, knowledgeSvc, gotKnowledge)
	assert.Same(t, auditSink, gotAudit)
}

func TestRegisterBackendRequiresBackendID(t *testing.T) {
	router := NewInMemoryRouter()

	err := router.RegisterBackend(BackendSet{TenantID: "tenant-a", BackendID: " "})

	require.ErrorIs(t, err, ErrBackendIDRequired)
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

func TestRouterRouteReturnsTenantScopedMetadata(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	p := profile("tenant-a", "profile-a", "hot")
	p.SessionBackend = "session-hot"
	p.MigrationMode = string(platform.StorageMigrationModeDualWrite)
	require.NoError(t, router.RegisterProfile(p))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "session-hot",
		Session:   sessioninmemory.NewSessionService(),
	}))

	route, err := router.Route(ctx, "tenant-a", "profile-a", platform.BackendMigrationResourceSession)
	require.NoError(t, err)

	assert.Equal(t, "tenant-a", route.TenantID)
	assert.Equal(t, "profile-a", route.ProfileID)
	assert.Equal(t, platform.BackendMigrationResourceSession, route.Resource)
	assert.Equal(t, "session-hot", route.BackendID)
	assert.Equal(t, "tenant/tenant-a/profile/profile-a", route.Namespace)
	assert.Equal(t, platform.StorageMigrationModeDualWrite, route.MigrationMode)
	assert.True(t, route.IsMigrating)
}

func TestRouterAdapterReturnsScopedStores(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	sessionSvc := sessioninmemory.NewSessionService()
	p := profile("tenant-a", "profile-a", "hot")
	p.SessionBackend = "session-hot"
	require.NoError(t, router.RegisterProfile(p))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "session-hot",
		Session:   sessionSvc,
	}))

	adapter, err := router.Adapter(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)
	sessionStore, err := adapter.Session(ctx)
	require.NoError(t, err)
	scopedApp := adapter.Scope().ScopedAppName("app-a")

	created, err := sessionStore.CreateSession(ctx, session.Key{
		AppName:   scopedApp,
		UserID:    "user-a",
		SessionID: "session-a",
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, "tenant/tenant-a/profile/profile-a/app-a", created.AppName)

	route, err := adapter.Route(ctx, platform.BackendMigrationResourceSession)
	require.NoError(t, err)
	assert.Equal(t, "tenant-a", route.TenantID)
	assert.Equal(t, "profile-a", route.ProfileID)
	assert.Equal(t, "tenant/tenant-a/profile/profile-a", route.Namespace)
}

func TestRouterAdapterPreservesSessionOptionalCapabilities(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	sessionSvc := &capabilitySessionService{
		Service: sessioninmemory.NewSessionService(),
	}
	p := profile("tenant-a", "profile-a", "hot")
	require.NoError(t, router.RegisterProfile(p))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "hot",
		Session:   sessionSvc,
	}))

	adapter, err := router.Adapter(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)
	sessionStore, err := adapter.Session(ctx)
	require.NoError(t, err)
	scopedApp := adapter.Scope().ScopedAppName("app-a")

	windowStore, ok := sessionStore.(session.WindowService)
	require.True(t, ok)
	searchStore, ok := sessionStore.(session.SearchableService)
	require.True(t, ok)
	trackStore, ok := sessionStore.(session.TrackService)
	require.True(t, ok)

	_, err = windowStore.GetEventWindow(ctx, session.EventWindowRequest{
		Key: session.Key{AppName: "app-a", UserID: "user-a", SessionID: "session-a"},
	})
	require.ErrorIs(t, err, ErrKeyOutsideTenantScope)
	_, err = searchStore.SearchEvents(ctx, session.EventSearchRequest{
		UserKey: session.UserKey{AppName: "app-a", UserID: "user-a"},
	})
	require.ErrorIs(t, err, ErrKeyOutsideTenantScope)
	err = trackStore.AppendTrackEvent(ctx, &session.Session{
		AppName:   "app-a",
		UserID:    "user-a",
		ID:        "session-a",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		State:     session.StateMap{},
	}, &session.TrackEvent{})
	require.ErrorIs(t, err, ErrKeyOutsideTenantScope)

	_, err = windowStore.GetEventWindow(ctx, session.EventWindowRequest{
		Key: session.Key{AppName: scopedApp, UserID: "user-a", SessionID: "session-a"},
	})
	require.NoError(t, err)
	_, err = searchStore.SearchEvents(ctx, session.EventSearchRequest{
		UserKey: session.UserKey{AppName: scopedApp, UserID: "user-a"},
	})
	require.NoError(t, err)
	err = trackStore.AppendTrackEvent(ctx, &session.Session{
		AppName:   scopedApp,
		UserID:    "user-a",
		ID:        "session-a",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		State:     session.StateMap{},
	}, &session.TrackEvent{})
	require.NoError(t, err)
	assert.True(t, sessionSvc.windowCalled)
	assert.True(t, sessionSvc.searchCalled)
	assert.True(t, sessionSvc.trackCalled)
}

func TestRouterAdapterRejectsUnscopedKeys(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	p := profile("tenant-a", "profile-a", "hot")
	require.NoError(t, router.RegisterProfile(p))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "hot",
		Session:   sessioninmemory.NewSessionService(),
		Memory:    memoryinmemory.NewMemoryService(),
	}))

	adapter, err := router.Adapter(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)
	sessionStore, err := adapter.Session(ctx)
	require.NoError(t, err)
	memoryStore, err := adapter.Memory(ctx)
	require.NoError(t, err)

	_, err = sessionStore.CreateSession(ctx, session.Key{
		AppName:   "app-a",
		UserID:    "user-a",
		SessionID: "session-a",
	}, nil)
	require.ErrorIs(t, err, ErrKeyOutsideTenantScope)

	err = memoryStore.AddMemory(ctx, memory.UserKey{
		AppName: "app-a",
		UserID:  "user-a",
	}, "prefers tea", []string{"preference"})
	require.ErrorIs(t, err, ErrKeyOutsideTenantScope)
}

type capabilitySessionService struct {
	session.Service
	windowCalled bool
	searchCalled bool
	trackCalled  bool
}

func (s *capabilitySessionService) GetEventWindow(
	ctx context.Context,
	req session.EventWindowRequest,
) (*session.EventWindow, error) {
	s.windowCalled = true
	return &session.EventWindow{SessionKey: req.Key}, nil
}

func (s *capabilitySessionService) SearchEvents(
	ctx context.Context,
	req session.EventSearchRequest,
) ([]session.EventSearchResult, error) {
	s.searchCalled = true
	return []session.EventSearchResult{}, nil
}

func (s *capabilitySessionService) AppendTrackEvent(
	ctx context.Context,
	sess *session.Session,
	event *session.TrackEvent,
	opts ...session.Option,
) error {
	s.trackCalled = true
	return nil
}

func TestRouterAdapterScopesKnowledgeQueries(t *testing.T) {
	ctx := context.Background()
	router := NewInMemoryRouter()
	knowledgeSvc := &capturingKnowledge{}
	p := profile("tenant-a", "profile-a", "hot")
	require.NoError(t, router.RegisterProfile(p))
	require.NoError(t, router.RegisterBackend(BackendSet{
		TenantID:  "tenant-a",
		BackendID: "hot",
		Knowledge: knowledgeSvc,
	}))
	adapter, err := router.Adapter(ctx, "tenant-a", "profile-a")
	require.NoError(t, err)
	knowledgeStore, err := adapter.Knowledge(ctx)
	require.NoError(t, err)
	req := &knowledge.SearchRequest{
		Query: "deployment",
		SearchFilter: &knowledge.SearchFilter{
			Metadata: map[string]any{"category": "runbook"},
		},
	}

	_, err = knowledgeStore.Search(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, "tenant-a", knowledgeSvc.last.SearchFilter.Metadata["tenant_id"])
	assert.Equal(t, "runbook", knowledgeSvc.last.SearchFilter.Metadata["category"])
	assert.NotContains(t, req.SearchFilter.Metadata, "tenant_id")

	_, err = knowledgeStore.Search(ctx, &knowledge.SearchRequest{
		Query: "deployment",
		SearchFilter: &knowledge.SearchFilter{
			Metadata: map[string]any{"tenant_id": "tenant-b"},
		},
	})
	require.ErrorIs(t, err, ErrKeyOutsideTenantScope)

	_, err = knowledgeStore.Search(ctx, &knowledge.SearchRequest{
		Query: "deployment",
		SearchFilter: &knowledge.SearchFilter{
			Metadata: map[string]any{"tenant_id": []string{"tenant-a"}},
		},
	})
	require.ErrorIs(t, err, ErrKeyOutsideTenantScope)
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
		SummaryBackend:   backendID,
		MemoryBackend:    backendID,
		ArtifactBackend:  backendID,
		KnowledgeBackend: backendID,
		AuditBackend:     backendID,
		DSNRef:           "secret://storage",
		Namespace:        "tenant/" + tenantID + "/profile/" + profileID,
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

type capturingKnowledge struct {
	last knowledge.SearchRequest
}

func (s *capturingKnowledge) Search(
	ctx context.Context,
	req *knowledge.SearchRequest,
) (*knowledge.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.last = *req
	return &knowledge.SearchResult{}, nil
}
