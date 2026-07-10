//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package memoryknowledge

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	"trpc.group/trpc-go/trpc-agent-go/platform"
)

const (
	// MetadataTenantID is the metadata key used to enforce tenant search scope.
	MetadataTenantID = "tenant_id"
	// MetadataAppID is the metadata key used to enforce app search scope.
	MetadataAppID = "app_id"
	// MetadataInternalUserID is the metadata key used to enforce internal user search scope.
	MetadataInternalUserID = "internal_user_id"
	// MetadataUserIDHash is the metadata key used to carry privacy-safe user identity.
	MetadataUserIDHash = "user_id_hash"
)

// Consistency describes when a memory write should become visible to retrieval.
type Consistency string

const (
	// ConsistencyEventual means the write was accepted but downstream vector/index
	// visibility may lag behind durable storage.
	ConsistencyEventual Consistency = "eventual"
)

// MemoryBackend is the long-term memory surface used by the facade.
type MemoryBackend interface {
	memory.Service
}

// KnowledgeBackend is the knowledge retrieval surface used by the facade.
// Implementations must honor SearchFilter.Metadata as mandatory filters.
type KnowledgeBackend interface {
	knowledge.Knowledge
}

// ServiceConfig wires concrete memory and knowledge backends. Tests can provide
// in-memory or mock backends, while production callers can wire vector stores.
type ServiceConfig struct {
	Memory    MemoryBackend
	Knowledge KnowledgeBackend
}

// Scope carries the tenant and privacy-safe user boundary for memory and RAG.
type Scope struct {
	TenantID       string
	AppID          string
	InternalUserID string
	UserIDHash     string
	Namespace      string
}

// Validate checks that the scope is strong enough for tenant/user isolation.
func (s Scope) Validate() error {
	if err := validateIdentifier(MetadataTenantID, s.TenantID, platform.ErrTenantIDRequired); err != nil {
		return err
	}
	if err := validateIdentifier(MetadataAppID, s.AppID, platform.ErrAppIDRequired); err != nil {
		return err
	}
	if err := validateIdentifier(MetadataInternalUserID, s.InternalUserID, ErrInternalUserIDRequired); err != nil {
		return err
	}
	if err := validateIdentifier("namespace", s.Namespace, ErrNamespaceRequired); err != nil {
		return err
	}
	if s.UserIDHash != "" {
		if err := validateIdentifier(MetadataUserIDHash, s.UserIDHash, nil); err != nil {
			return err
		}
	}
	if !namespaceContainsSegment(s.Namespace, s.TenantID) {
		return fmt.Errorf("namespace must include tenant_id")
	}
	return nil
}

// ScopedAppName returns the memory app key inside the tenant namespace.
func (s Scope) ScopedAppName() string {
	namespace := strings.TrimRight(strings.TrimSpace(s.Namespace), `/\|:`)
	appID := strings.Trim(strings.TrimSpace(s.AppID), `/\|:`)
	if namespace == "" {
		return appID
	}
	if appID == "" {
		return namespace
	}
	return namespace + "/" + appID
}

func (s Scope) memoryUserKey() memory.UserKey {
	return memory.UserKey{
		AppName: s.ScopedAppName(),
		UserID:  s.InternalUserID,
	}
}

// MemoryWriteRequest writes one long-term memory in a scoped backend.
type MemoryWriteRequest struct {
	Scope    Scope
	Memory   string
	Topics   []string
	Metadata *memory.Metadata
}

// MemoryWriteReceipt confirms acceptance without promising immediate retrieval visibility.
type MemoryWriteReceipt struct {
	TenantID       string
	AppID          string
	InternalUserID string
	UserIDHash     string
	AppName        string
	Accepted       bool
	Consistency    Consistency
}

// KnowledgeSearchRequest wraps a knowledge request with mandatory tenant/user scope.
type KnowledgeSearchRequest struct {
	Scope   Scope
	Request *knowledge.SearchRequest
}

// Service enforces tenant/internal-user scope across memory writes and retrieval.
type Service struct {
	memory    MemoryBackend
	knowledge KnowledgeBackend
}

// New creates a scoped memory and knowledge service facade.
func New(config ServiceConfig) (*Service, error) {
	if config.Memory == nil {
		return nil, ErrMemoryBackendRequired
	}
	if config.Knowledge == nil {
		return nil, ErrKnowledgeBackendRequired
	}
	return &Service{
		memory:    config.Memory,
		knowledge: config.Knowledge,
	}, nil
}

// AddMemory accepts a scoped memory write. The receipt is intentionally eventual:
// callers should not assume vector/search visibility before a later retrieval cycle.
func (s *Service) AddMemory(
	ctx context.Context,
	req MemoryWriteRequest,
) (MemoryWriteReceipt, error) {
	if err := ctx.Err(); err != nil {
		return MemoryWriteReceipt{}, err
	}
	if err := req.Scope.Validate(); err != nil {
		return MemoryWriteReceipt{}, err
	}
	opts := make([]memory.AddOption, 0, 1)
	if req.Metadata != nil {
		opts = append(opts, memory.WithMetadata(req.Metadata))
	}
	topics := append([]string(nil), req.Topics...)
	if err := s.memory.AddMemory(ctx, req.Scope.memoryUserKey(), req.Memory, topics, opts...); err != nil {
		return MemoryWriteReceipt{}, err
	}
	return MemoryWriteReceipt{
		TenantID:       req.Scope.TenantID,
		AppID:          req.Scope.AppID,
		InternalUserID: req.Scope.InternalUserID,
		UserIDHash:     req.Scope.UserIDHash,
		AppName:        req.Scope.ScopedAppName(),
		Accepted:       true,
		Consistency:    ConsistencyEventual,
	}, nil
}

// ReadMemories reads memories inside the tenant/internal-user boundary.
func (s *Service) ReadMemories(
	ctx context.Context,
	scope Scope,
	limit int,
) ([]*memory.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return s.memory.ReadMemories(ctx, scope.memoryUserKey(), limit)
}

// SearchMemories searches memories inside the tenant/internal-user boundary.
func (s *Service) SearchMemories(
	ctx context.Context,
	scope Scope,
	query string,
	opts ...memory.SearchOption,
) ([]*memory.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	return s.memory.SearchMemories(ctx, scope.memoryUserKey(), query, opts...)
}

// SearchKnowledge injects tenant/internal-user filters into a cloned request.
// MaxResults and MinScore remain caller-controlled knobs for latency, cost, and
// recall tradeoffs; the scope filters are mandatory regardless of those choices.
func (s *Service) SearchKnowledge(
	ctx context.Context,
	req KnowledgeSearchRequest,
) (*knowledge.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := req.Scope.Validate(); err != nil {
		return nil, err
	}
	if req.Request == nil {
		return nil, fmt.Errorf("knowledge search request is required")
	}
	scopedReq := cloneKnowledgeRequest(req.Request)
	if strings.TrimSpace(scopedReq.UserID) != "" && scopedReq.UserID != req.Scope.InternalUserID {
		return nil, ErrFilterOutsideScope
	}
	scopedReq.UserID = req.Scope.InternalUserID
	metadata := scopedReq.SearchFilter.Metadata
	for key, value := range map[string]string{
		MetadataTenantID:       req.Scope.TenantID,
		MetadataAppID:          req.Scope.AppID,
		MetadataInternalUserID: req.Scope.InternalUserID,
	} {
		if err := enforceMetadata(metadata, key, value); err != nil {
			return nil, err
		}
	}
	if req.Scope.UserIDHash != "" {
		if err := enforceMetadata(metadata, MetadataUserIDHash, req.Scope.UserIDHash); err != nil {
			return nil, err
		}
	}
	return s.knowledge.Search(ctx, &scopedReq)
}

func cloneKnowledgeRequest(req *knowledge.SearchRequest) knowledge.SearchRequest {
	scopedReq := *req
	if req.SearchFilter == nil {
		scopedReq.SearchFilter = &knowledge.SearchFilter{
			Metadata: make(map[string]any, 4),
		}
		return scopedReq
	}
	filter := *req.SearchFilter
	if req.SearchFilter.DocumentIDs != nil {
		filter.DocumentIDs = append([]string(nil), req.SearchFilter.DocumentIDs...)
	}
	filter.Metadata = make(map[string]any, len(req.SearchFilter.Metadata)+4)
	for key, value := range req.SearchFilter.Metadata {
		filter.Metadata[key] = value
	}
	scopedReq.SearchFilter = &filter
	return scopedReq
}

func enforceMetadata(metadata map[string]any, key string, value string) error {
	if existing, ok := metadata[key]; ok {
		existingText, ok := existing.(string)
		if !ok || existingText != value {
			return ErrFilterOutsideScope
		}
	}
	metadata[key] = value
	return nil
}

func validateIdentifier(field string, value string, requiredErr error) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		if requiredErr == nil {
			return fmt.Errorf("%s must not be blank", field)
		}
		return requiredErr
	}
	if trimmed != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s must not contain control characters", field)
		}
	}
	return nil
}

func namespaceContainsSegment(namespace, tenantID string) bool {
	for _, segment := range strings.FieldsFunc(namespace, func(r rune) bool {
		switch r {
		case '/', '\\', ':', '|':
			return true
		default:
			return false
		}
	}) {
		if segment == tenantID {
			return true
		}
	}
	return false
}
