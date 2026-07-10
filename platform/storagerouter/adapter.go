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
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/artifact"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

const tenantMetadataKey = "tenant_id"

// StorageAdapter is a tenant/profile-bound storage facade.
type StorageAdapter interface {
	Scope() StorageScope
	Route(ctx context.Context, resource platform.BackendMigrationResource) (RouteBinding, error)
	Session(ctx context.Context) (session.Service, error)
	Summary(ctx context.Context) (SummaryStore, error)
	Memory(ctx context.Context) (memory.Service, error)
	Artifact(ctx context.Context) (artifact.Service, error)
	Knowledge(ctx context.Context) (knowledge.Knowledge, error)
	Audit(ctx context.Context) (platform.AuditSink, error)
}

// StorageScope describes the tenant boundary applied by a StorageAdapter.
type StorageScope struct {
	TenantID  string
	ProfileID string
	Namespace string
}

// ScopedAppName returns an app name prefixed with the tenant-scoped storage namespace.
func (s StorageScope) ScopedAppName(appName string) string {
	prefix := s.namespacePrefix()
	appName = strings.Trim(strings.TrimSpace(appName), `/\|:`)
	if appName == "" {
		return strings.TrimSuffix(prefix, "/")
	}
	if strings.HasPrefix(appName, prefix) {
		return appName
	}
	return prefix + appName
}

func (s StorageScope) namespacePrefix() string {
	namespace := strings.TrimRight(strings.TrimSpace(s.Namespace), `/\|:`)
	if namespace == "" {
		return ""
	}
	return namespace + "/"
}

func (s StorageScope) validateAppName(appName string) error {
	if strings.TrimSpace(appName) == "" || strings.TrimSpace(appName) != appName {
		return ErrKeyOutsideTenantScope
	}
	prefix := s.namespacePrefix()
	if prefix == "" || !strings.HasPrefix(appName, prefix) {
		return ErrKeyOutsideTenantScope
	}
	if strings.TrimSpace(strings.TrimPrefix(appName, prefix)) == "" {
		return ErrKeyOutsideTenantScope
	}
	return nil
}

func (s StorageScope) validateSessionKey(key session.Key) error {
	if err := key.CheckSessionKey(); err != nil {
		return err
	}
	return s.validateAppName(key.AppName)
}

func (s StorageScope) validateSessionUserKey(key session.UserKey) error {
	if err := key.CheckUserKey(); err != nil {
		return err
	}
	return s.validateAppName(key.AppName)
}

func (s StorageScope) validateSession(sess *session.Session) error {
	if sess == nil {
		return session.ErrNilSession
	}
	return s.validateAppName(sess.AppName)
}

func (s StorageScope) validateMemoryKey(key memory.Key) error {
	if err := key.CheckMemoryKey(); err != nil {
		return err
	}
	return s.validateAppName(key.AppName)
}

func (s StorageScope) validateMemoryUserKey(key memory.UserKey) error {
	if err := key.CheckUserKey(); err != nil {
		return err
	}
	return s.validateAppName(key.AppName)
}

func (s StorageScope) validateArtifactSessionInfo(info artifact.SessionInfo) error {
	if strings.TrimSpace(info.UserID) == "" || strings.TrimSpace(info.SessionID) == "" {
		return ErrKeyOutsideTenantScope
	}
	return s.validateAppName(info.AppName)
}

type tenantStorageAdapter struct {
	router *InMemoryRouter
	scope  StorageScope
}

func (a *tenantStorageAdapter) Scope() StorageScope {
	return a.scope
}

func (a *tenantStorageAdapter) Route(
	ctx context.Context,
	resource platform.BackendMigrationResource,
) (RouteBinding, error) {
	return a.router.Route(ctx, a.scope.TenantID, a.scope.ProfileID, resource)
}

func (a *tenantStorageAdapter) Session(ctx context.Context) (session.Service, error) {
	service, err := a.router.Session(ctx, a.scope.TenantID, a.scope.ProfileID)
	if err != nil {
		return nil, err
	}
	return &scopedSessionService{Service: service, scope: a.scope}, nil
}

func (a *tenantStorageAdapter) Summary(ctx context.Context) (SummaryStore, error) {
	store, err := a.router.Summary(ctx, a.scope.TenantID, a.scope.ProfileID)
	if err != nil {
		return nil, err
	}
	return &scopedSummaryStore{SummaryStore: store, scope: a.scope}, nil
}

func (a *tenantStorageAdapter) Memory(ctx context.Context) (memory.Service, error) {
	service, err := a.router.Memory(ctx, a.scope.TenantID, a.scope.ProfileID)
	if err != nil {
		return nil, err
	}
	return &scopedMemoryService{Service: service, scope: a.scope}, nil
}

func (a *tenantStorageAdapter) Artifact(ctx context.Context) (artifact.Service, error) {
	service, err := a.router.Artifact(ctx, a.scope.TenantID, a.scope.ProfileID)
	if err != nil {
		return nil, err
	}
	return &scopedArtifactService{Service: service, scope: a.scope}, nil
}

func (a *tenantStorageAdapter) Knowledge(ctx context.Context) (knowledge.Knowledge, error) {
	service, err := a.router.Knowledge(ctx, a.scope.TenantID, a.scope.ProfileID)
	if err != nil {
		return nil, err
	}
	return &scopedKnowledge{Knowledge: service, scope: a.scope}, nil
}

func (a *tenantStorageAdapter) Audit(ctx context.Context) (platform.AuditSink, error) {
	sink, err := a.router.Audit(ctx, a.scope.TenantID, a.scope.ProfileID)
	if err != nil {
		return nil, err
	}
	return &scopedAuditSink{AuditSink: sink, scope: a.scope}, nil
}

type scopedSessionService struct {
	session.Service
	scope StorageScope
}

func (s *scopedSessionService) CreateSession(
	ctx context.Context,
	key session.Key,
	state session.StateMap,
	options ...session.Option,
) (*session.Session, error) {
	if err := s.scope.validateSessionKey(key); err != nil {
		return nil, err
	}
	return s.Service.CreateSession(ctx, key, state, options...)
}

func (s *scopedSessionService) GetSession(
	ctx context.Context,
	key session.Key,
	options ...session.Option,
) (*session.Session, error) {
	if err := s.scope.validateSessionKey(key); err != nil {
		return nil, err
	}
	return s.Service.GetSession(ctx, key, options...)
}

func (s *scopedSessionService) ListSessions(
	ctx context.Context,
	userKey session.UserKey,
	options ...session.Option,
) ([]*session.Session, error) {
	if err := s.scope.validateSessionUserKey(userKey); err != nil {
		return nil, err
	}
	return s.Service.ListSessions(ctx, userKey, options...)
}

func (s *scopedSessionService) DeleteSession(
	ctx context.Context,
	key session.Key,
	options ...session.Option,
) error {
	if err := s.scope.validateSessionKey(key); err != nil {
		return err
	}
	return s.Service.DeleteSession(ctx, key, options...)
}

func (s *scopedSessionService) UpdateAppState(
	ctx context.Context,
	appName string,
	state session.StateMap,
) error {
	if err := s.scope.validateAppName(appName); err != nil {
		return err
	}
	return s.Service.UpdateAppState(ctx, appName, state)
}

func (s *scopedSessionService) DeleteAppState(ctx context.Context, appName string, key string) error {
	if err := s.scope.validateAppName(appName); err != nil {
		return err
	}
	return s.Service.DeleteAppState(ctx, appName, key)
}

func (s *scopedSessionService) ListAppStates(ctx context.Context, appName string) (session.StateMap, error) {
	if err := s.scope.validateAppName(appName); err != nil {
		return nil, err
	}
	return s.Service.ListAppStates(ctx, appName)
}

func (s *scopedSessionService) UpdateUserState(
	ctx context.Context,
	userKey session.UserKey,
	state session.StateMap,
) error {
	if err := s.scope.validateSessionUserKey(userKey); err != nil {
		return err
	}
	return s.Service.UpdateUserState(ctx, userKey, state)
}

func (s *scopedSessionService) ListUserStates(
	ctx context.Context,
	userKey session.UserKey,
) (session.StateMap, error) {
	if err := s.scope.validateSessionUserKey(userKey); err != nil {
		return nil, err
	}
	return s.Service.ListUserStates(ctx, userKey)
}

func (s *scopedSessionService) DeleteUserState(
	ctx context.Context,
	userKey session.UserKey,
	key string,
) error {
	if err := s.scope.validateSessionUserKey(userKey); err != nil {
		return err
	}
	return s.Service.DeleteUserState(ctx, userKey, key)
}

func (s *scopedSessionService) UpdateSessionState(
	ctx context.Context,
	key session.Key,
	state session.StateMap,
) error {
	if err := s.scope.validateSessionKey(key); err != nil {
		return err
	}
	return s.Service.UpdateSessionState(ctx, key, state)
}

func (s *scopedSessionService) AppendEvent(
	ctx context.Context,
	sess *session.Session,
	event *event.Event,
	options ...session.Option,
) error {
	if err := s.scope.validateSession(sess); err != nil {
		return err
	}
	return s.Service.AppendEvent(ctx, sess, event, options...)
}

func (s *scopedSessionService) CreateSessionSummary(
	ctx context.Context,
	sess *session.Session,
	filterKey string,
	force bool,
) error {
	if err := s.scope.validateSession(sess); err != nil {
		return err
	}
	return s.Service.CreateSessionSummary(ctx, sess, filterKey, force)
}

func (s *scopedSessionService) EnqueueSummaryJob(
	ctx context.Context,
	sess *session.Session,
	filterKey string,
	force bool,
) error {
	if err := s.scope.validateSession(sess); err != nil {
		return err
	}
	return s.Service.EnqueueSummaryJob(ctx, sess, filterKey, force)
}

func (s *scopedSessionService) GetSessionSummaryText(
	ctx context.Context,
	sess *session.Session,
	opts ...session.SummaryOption,
) (string, bool) {
	if err := s.scope.validateSession(sess); err != nil {
		return "", false
	}
	return s.Service.GetSessionSummaryText(ctx, sess, opts...)
}

type scopedSummaryStore struct {
	SummaryStore
	scope StorageScope
}

func (s *scopedSummaryStore) CreateSessionSummary(
	ctx context.Context,
	sess *session.Session,
	filterKey string,
	force bool,
) error {
	if err := s.scope.validateSession(sess); err != nil {
		return err
	}
	return s.SummaryStore.CreateSessionSummary(ctx, sess, filterKey, force)
}

func (s *scopedSummaryStore) EnqueueSummaryJob(
	ctx context.Context,
	sess *session.Session,
	filterKey string,
	force bool,
) error {
	if err := s.scope.validateSession(sess); err != nil {
		return err
	}
	return s.SummaryStore.EnqueueSummaryJob(ctx, sess, filterKey, force)
}

func (s *scopedSummaryStore) GetSessionSummaryText(
	ctx context.Context,
	sess *session.Session,
	opts ...session.SummaryOption,
) (string, bool) {
	if err := s.scope.validateSession(sess); err != nil {
		return "", false
	}
	return s.SummaryStore.GetSessionSummaryText(ctx, sess, opts...)
}

type scopedMemoryService struct {
	memory.Service
	scope StorageScope
}

func (s *scopedMemoryService) ReadMemories(
	ctx context.Context,
	userKey memory.UserKey,
	limit int,
) ([]*memory.Entry, error) {
	if err := s.scope.validateMemoryUserKey(userKey); err != nil {
		return nil, err
	}
	return s.Service.ReadMemories(ctx, userKey, limit)
}

func (s *scopedMemoryService) SearchMemories(
	ctx context.Context,
	userKey memory.UserKey,
	query string,
	opts ...memory.SearchOption,
) ([]*memory.Entry, error) {
	if err := s.scope.validateMemoryUserKey(userKey); err != nil {
		return nil, err
	}
	return s.Service.SearchMemories(ctx, userKey, query, opts...)
}

func (s *scopedMemoryService) AddMemory(
	ctx context.Context,
	userKey memory.UserKey,
	mem string,
	topics []string,
	opts ...memory.AddOption,
) error {
	if err := s.scope.validateMemoryUserKey(userKey); err != nil {
		return err
	}
	return s.Service.AddMemory(ctx, userKey, mem, topics, opts...)
}

func (s *scopedMemoryService) UpdateMemory(
	ctx context.Context,
	memoryKey memory.Key,
	mem string,
	topics []string,
	opts ...memory.UpdateOption,
) error {
	if err := s.scope.validateMemoryKey(memoryKey); err != nil {
		return err
	}
	return s.Service.UpdateMemory(ctx, memoryKey, mem, topics, opts...)
}

func (s *scopedMemoryService) DeleteMemory(ctx context.Context, memoryKey memory.Key) error {
	if err := s.scope.validateMemoryKey(memoryKey); err != nil {
		return err
	}
	return s.Service.DeleteMemory(ctx, memoryKey)
}

func (s *scopedMemoryService) ClearMemories(ctx context.Context, userKey memory.UserKey) error {
	if err := s.scope.validateMemoryUserKey(userKey); err != nil {
		return err
	}
	return s.Service.ClearMemories(ctx, userKey)
}

func (s *scopedMemoryService) EnqueueAutoMemoryJob(
	ctx context.Context,
	sess *session.Session,
) error {
	if err := s.scope.validateSession(sess); err != nil {
		return err
	}
	return s.Service.EnqueueAutoMemoryJob(ctx, sess)
}

type scopedArtifactService struct {
	artifact.Service
	scope StorageScope
}

func (s *scopedArtifactService) SaveArtifact(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
	filename string,
	artifactValue *artifact.Artifact,
) (int, error) {
	if err := s.scope.validateArtifactSessionInfo(sessionInfo); err != nil {
		return 0, err
	}
	return s.Service.SaveArtifact(ctx, sessionInfo, filename, artifactValue)
}

func (s *scopedArtifactService) LoadArtifact(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
	filename string,
	version *int,
) (*artifact.Artifact, error) {
	if err := s.scope.validateArtifactSessionInfo(sessionInfo); err != nil {
		return nil, err
	}
	return s.Service.LoadArtifact(ctx, sessionInfo, filename, version)
}

func (s *scopedArtifactService) ListArtifactKeys(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
) ([]string, error) {
	if err := s.scope.validateArtifactSessionInfo(sessionInfo); err != nil {
		return nil, err
	}
	return s.Service.ListArtifactKeys(ctx, sessionInfo)
}

func (s *scopedArtifactService) DeleteArtifact(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
	filename string,
) error {
	if err := s.scope.validateArtifactSessionInfo(sessionInfo); err != nil {
		return err
	}
	return s.Service.DeleteArtifact(ctx, sessionInfo, filename)
}

func (s *scopedArtifactService) ListVersions(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
	filename string,
) ([]int, error) {
	if err := s.scope.validateArtifactSessionInfo(sessionInfo); err != nil {
		return nil, err
	}
	return s.Service.ListVersions(ctx, sessionInfo, filename)
}

type scopedKnowledge struct {
	knowledge.Knowledge
	scope StorageScope
}

func (s *scopedKnowledge) Search(
	ctx context.Context,
	req *knowledge.SearchRequest,
) (*knowledge.SearchResult, error) {
	if req == nil {
		return nil, errors.New("knowledge search request is required")
	}
	scopedReq := *req
	if req.SearchFilter == nil {
		scopedReq.SearchFilter = &knowledge.SearchFilter{}
	} else {
		filter := *req.SearchFilter
		scopedReq.SearchFilter = &filter
	}
	if scopedReq.SearchFilter.Metadata == nil {
		scopedReq.SearchFilter.Metadata = make(map[string]any, 1)
	} else {
		metadata := make(map[string]any, len(scopedReq.SearchFilter.Metadata)+1)
		for key, value := range scopedReq.SearchFilter.Metadata {
			metadata[key] = value
		}
		scopedReq.SearchFilter.Metadata = metadata
	}
	if tenantID, ok := scopedReq.SearchFilter.Metadata[tenantMetadataKey]; ok && tenantID != s.scope.TenantID {
		return nil, ErrKeyOutsideTenantScope
	}
	scopedReq.SearchFilter.Metadata[tenantMetadataKey] = s.scope.TenantID
	return s.Knowledge.Search(ctx, &scopedReq)
}

type scopedAuditSink struct {
	platform.AuditSink
	scope StorageScope
}

func (s *scopedAuditSink) WriteAudit(ctx context.Context, record platform.AuditRecord) error {
	if strings.TrimSpace(record.TenantID) != s.scope.TenantID {
		return ErrKeyOutsideTenantScope
	}
	return s.AuditSink.WriteAudit(ctx, record)
}
