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
	"fmt"
	"strings"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/artifact"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

// BackendSet groups concrete services for one storage backend registration.
type BackendSet struct {
	TenantID  string
	BackendID string
	Session   session.Service
	Summary   SummaryStore
	Memory    memory.Service
	Artifact  artifact.Service
	Knowledge knowledge.Knowledge
	Audit     platform.AuditSink
}

// SummaryStore is the summary-specific storage surface selected by SummaryBackend.
type SummaryStore interface {
	CreateSessionSummary(ctx context.Context, sess *session.Session, filterKey string, force bool) error
	EnqueueSummaryJob(ctx context.Context, sess *session.Session, filterKey string, force bool) error
	GetSessionSummaryText(ctx context.Context, sess *session.Session, opts ...session.SummaryOption) (string, bool)
}

// RouteBinding describes the concrete backend route selected for one resource.
type RouteBinding struct {
	TenantID      string
	ProfileID     string
	Resource      platform.BackendMigrationResource
	BackendID     string
	Namespace     string
	MigrationMode platform.StorageMigrationMode
	IsMigrating   bool
}

// Router resolves tenant/app storage services from platform storage profiles.
type Router interface {
	Profile(ctx context.Context, tenantID string, profileID string) (platform.StorageProfile, error)
	Adapter(ctx context.Context, tenantID string, profileID string) (StorageAdapter, error)
	Route(ctx context.Context, tenantID string, profileID string, resource platform.BackendMigrationResource) (RouteBinding, error)
	Session(ctx context.Context, tenantID string, profileID string) (session.Service, error)
	Summary(ctx context.Context, tenantID string, profileID string) (SummaryStore, error)
	Memory(ctx context.Context, tenantID string, profileID string) (memory.Service, error)
	Artifact(ctx context.Context, tenantID string, profileID string) (artifact.Service, error)
	Knowledge(ctx context.Context, tenantID string, profileID string) (knowledge.Knowledge, error)
	Audit(ctx context.Context, tenantID string, profileID string) (platform.AuditSink, error)
	Status(ctx context.Context, tenantID string, profileID string) (StatusSummary, error)
}

// InMemoryRouter is a concurrency-safe storage router for tests and demos.
type InMemoryRouter struct {
	mu       sync.RWMutex
	profiles map[profileKey]platform.StorageProfile
	backends map[backendKey]BackendSet
}

type profileKey struct {
	tenantID  string
	profileID string
}

type backendKey struct {
	tenantID  string
	backendID string
}

// NewInMemoryRouter creates an empty in-memory storage router.
func NewInMemoryRouter() *InMemoryRouter {
	return &InMemoryRouter{
		profiles: make(map[profileKey]platform.StorageProfile),
		backends: make(map[backendKey]BackendSet),
	}
}

// RegisterProfile registers or replaces one tenant storage profile.
func (r *InMemoryRouter) RegisterProfile(profile platform.StorageProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[profileKey{
		tenantID:  profile.TenantID,
		profileID: profile.ProfileID,
	}] = profile
	return nil
}

// RegisterBackend registers or replaces one concrete backend set.
func (r *InMemoryRouter) RegisterBackend(backend BackendSet) error {
	if strings.TrimSpace(backend.TenantID) == "" {
		return platform.ErrTenantIDRequired
	}
	if strings.TrimSpace(backend.BackendID) == "" {
		return ErrBackendIDRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[backendKey{
		tenantID:  backend.TenantID,
		backendID: backend.BackendID,
	}] = backend
	return nil
}

// Profile resolves one tenant storage profile.
func (r *InMemoryRouter) Profile(
	ctx context.Context,
	tenantID string,
	profileID string,
) (platform.StorageProfile, error) {
	if err := ctx.Err(); err != nil {
		return platform.StorageProfile{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	profile, ok := r.profiles[profileKey{tenantID: tenantID, profileID: profileID}]
	if !ok {
		return platform.StorageProfile{}, ErrProfileNotFound
	}
	// Defensive for future persistent backends that may not key by tenant_id.
	if profile.TenantID != tenantID {
		return platform.StorageProfile{}, ErrTenantMismatch
	}
	return profile, nil
}

// Adapter returns a tenant/profile-bound storage adapter.
func (r *InMemoryRouter) Adapter(
	ctx context.Context,
	tenantID string,
	profileID string,
) (StorageAdapter, error) {
	profile, err := r.Profile(ctx, tenantID, profileID)
	if err != nil {
		return nil, err
	}
	return &tenantStorageAdapter{
		router: r,
		scope: StorageScope{
			TenantID:  profile.TenantID,
			ProfileID: profile.ProfileID,
			Namespace: profile.Namespace,
		},
	}, nil
}

// Route resolves the concrete tenant-scoped backend route for one resource.
func (r *InMemoryRouter) Route(
	ctx context.Context,
	tenantID string,
	profileID string,
	resource platform.BackendMigrationResource,
) (RouteBinding, error) {
	profile, err := r.Profile(ctx, tenantID, profileID)
	if err != nil {
		return RouteBinding{}, err
	}
	kind, err := resourceKindFor(resource)
	if err != nil {
		return RouteBinding{}, err
	}
	backendID := backendIDFor(profile, kind)
	if strings.TrimSpace(backendID) == "" {
		return RouteBinding{}, ErrBackendNotFound
	}
	mode, err := platform.NormalizeStorageMigrationMode(profile.MigrationMode)
	if err != nil {
		return RouteBinding{}, err
	}
	r.mu.RLock()
	backend, ok := r.backends[backendKey{tenantID: tenantID, backendID: backendID}]
	r.mu.RUnlock()
	if !ok {
		return RouteBinding{}, ErrBackendNotFound
	}
	if backend.TenantID != tenantID {
		return RouteBinding{}, ErrBackendTenantMismatch
	}
	if !backendHasResource(backend, kind) {
		return RouteBinding{}, ErrBackendNotFound
	}
	return RouteBinding{
		TenantID:      profile.TenantID,
		ProfileID:     profile.ProfileID,
		Resource:      resource,
		BackendID:     strings.TrimSpace(backendID),
		Namespace:     profile.Namespace,
		MigrationMode: mode,
		IsMigrating:   platform.IsActiveStorageMigrationMode(mode),
	}, nil
}

// Session resolves the session service selected by a tenant storage profile.
func (r *InMemoryRouter) Session(
	ctx context.Context,
	tenantID string,
	profileID string,
) (session.Service, error) {
	backend, err := r.backend(ctx, tenantID, profileID, resourceSession)
	if err != nil {
		return nil, err
	}
	if backend.Session == nil {
		return nil, ErrBackendNotFound
	}
	return backend.Session, nil
}

// Summary resolves the summary store selected by a tenant storage profile.
func (r *InMemoryRouter) Summary(
	ctx context.Context,
	tenantID string,
	profileID string,
) (SummaryStore, error) {
	backend, err := r.backend(ctx, tenantID, profileID, resourceSummary)
	if err != nil {
		return nil, err
	}
	if backend.Summary == nil {
		return nil, ErrBackendNotFound
	}
	return backend.Summary, nil
}

// Memory resolves the memory service selected by a tenant storage profile.
func (r *InMemoryRouter) Memory(
	ctx context.Context,
	tenantID string,
	profileID string,
) (memory.Service, error) {
	backend, err := r.backend(ctx, tenantID, profileID, resourceMemory)
	if err != nil {
		return nil, err
	}
	if backend.Memory == nil {
		return nil, ErrBackendNotFound
	}
	return backend.Memory, nil
}

// Artifact resolves the artifact service selected by a tenant storage profile.
func (r *InMemoryRouter) Artifact(
	ctx context.Context,
	tenantID string,
	profileID string,
) (artifact.Service, error) {
	backend, err := r.backend(ctx, tenantID, profileID, resourceArtifact)
	if err != nil {
		return nil, err
	}
	if backend.Artifact == nil {
		return nil, ErrBackendNotFound
	}
	return backend.Artifact, nil
}

// Knowledge resolves the knowledge service selected by a tenant storage profile.
func (r *InMemoryRouter) Knowledge(
	ctx context.Context,
	tenantID string,
	profileID string,
) (knowledge.Knowledge, error) {
	backend, err := r.backend(ctx, tenantID, profileID, resourceKnowledge)
	if err != nil {
		return nil, err
	}
	if backend.Knowledge == nil {
		return nil, ErrBackendNotFound
	}
	return backend.Knowledge, nil
}

// Audit resolves the audit sink selected by a tenant storage profile.
func (r *InMemoryRouter) Audit(
	ctx context.Context,
	tenantID string,
	profileID string,
) (platform.AuditSink, error) {
	backend, err := r.backend(ctx, tenantID, profileID, resourceAudit)
	if err != nil {
		return nil, err
	}
	if backend.Audit == nil {
		return nil, ErrBackendNotFound
	}
	return backend.Audit, nil
}

type resourceKind string

const (
	resourceSession   resourceKind = "session"
	resourceSummary   resourceKind = "summary"
	resourceMemory    resourceKind = "memory"
	resourceArtifact  resourceKind = "artifact"
	resourceKnowledge resourceKind = "knowledge"
	resourceAudit     resourceKind = "audit"
)

func (r *InMemoryRouter) backend(
	ctx context.Context,
	tenantID string,
	profileID string,
	kind resourceKind,
) (BackendSet, error) {
	profile, err := r.Profile(ctx, tenantID, profileID)
	if err != nil {
		return BackendSet{}, err
	}
	backendID := backendIDFor(profile, kind)
	if backendID == "" {
		return BackendSet{}, ErrBackendNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	backend, ok := r.backends[backendKey{tenantID: tenantID, backendID: backendID}]
	if !ok {
		return BackendSet{}, ErrBackendNotFound
	}
	// Defensive for future persistent backends that may not key by tenant_id.
	if backend.TenantID != tenantID {
		return BackendSet{}, ErrBackendTenantMismatch
	}
	return backend, nil
}

func backendIDFor(profile platform.StorageProfile, kind resourceKind) string {
	switch kind {
	case resourceSession:
		return strings.TrimSpace(profile.SessionBackend)
	case resourceSummary:
		return strings.TrimSpace(profile.SummaryBackend)
	case resourceMemory:
		return strings.TrimSpace(profile.MemoryBackend)
	case resourceArtifact:
		return strings.TrimSpace(profile.ArtifactBackend)
	case resourceKnowledge:
		return strings.TrimSpace(profile.KnowledgeBackend)
	case resourceAudit:
		return strings.TrimSpace(profile.AuditBackend)
	default:
		return ""
	}
}

func resourceKindFor(resource platform.BackendMigrationResource) (resourceKind, error) {
	switch resource {
	case platform.BackendMigrationResourceSession:
		return resourceSession, nil
	case platform.BackendMigrationResourceSummary:
		return resourceSummary, nil
	case platform.BackendMigrationResourceMemory:
		return resourceMemory, nil
	case platform.BackendMigrationResourceArtifact:
		return resourceArtifact, nil
	case platform.BackendMigrationResourceKnowledge:
		return resourceKnowledge, nil
	case platform.BackendMigrationResourceAudit:
		return resourceAudit, nil
	default:
		return "", fmt.Errorf("unsupported storage resource %q", resource)
	}
}
