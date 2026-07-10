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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	memoryinmemory "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
)

func TestServiceAcceptsEventualMemoryWriteAndScopesByTenantUser(t *testing.T) {
	ctx := context.Background()
	service, err := New(ServiceConfig{
		Memory:    memoryinmemory.NewMemoryService(),
		Knowledge: &capturingKnowledge{},
	})
	require.NoError(t, err)
	scope := Scope{
		TenantID:       "tenant-a",
		AppID:          "app-a",
		InternalUserID: "internal-user-a",
		UserIDHash:     "hash-a",
		Namespace:      "tenant/tenant-a",
	}

	receipt, err := service.AddMemory(ctx, MemoryWriteRequest{
		Scope:  scope,
		Memory: "Prefers concise deployment runbooks.",
		Topics: []string{"preference", "runbook"},
	})
	require.NoError(t, err)

	assert.True(t, receipt.Accepted)
	assert.Equal(t, ConsistencyEventual, receipt.Consistency)
	assert.Equal(t, "tenant/tenant-a/app-a", receipt.AppName)
	assert.Equal(t, "internal-user-a", receipt.InternalUserID)
	assert.Equal(t, "hash-a", receipt.UserIDHash)

	entries, err := service.SearchMemories(ctx, scope, "deployment")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "tenant/tenant-a/app-a", entries[0].AppName)
	assert.Equal(t, "internal-user-a", entries[0].UserID)

	otherTenant := scope
	otherTenant.TenantID = "tenant-b"
	otherTenant.Namespace = "tenant/tenant-b"
	entries, err = service.SearchMemories(ctx, otherTenant, "deployment")
	require.NoError(t, err)
	assert.Empty(t, entries)

	otherUser := scope
	otherUser.InternalUserID = "internal-user-b"
	entries, err = service.SearchMemories(ctx, otherUser, "deployment")
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestServiceRejectsMemoryReadsWithoutInternalUserScope(t *testing.T) {
	ctx := context.Background()
	service, err := New(ServiceConfig{
		Memory:    memoryinmemory.NewMemoryService(),
		Knowledge: &capturingKnowledge{},
	})
	require.NoError(t, err)

	_, err = service.ReadMemories(ctx, Scope{
		TenantID:  "tenant-a",
		AppID:     "app-a",
		Namespace: "tenant/tenant-a",
	}, 10)

	require.ErrorIs(t, err, ErrInternalUserIDRequired)
}

func TestServiceRejectsUnsafeUserIDHashScope(t *testing.T) {
	ctx := context.Background()
	service, err := New(ServiceConfig{
		Memory:    memoryinmemory.NewMemoryService(),
		Knowledge: &capturingKnowledge{},
	})
	require.NoError(t, err)

	_, err = service.SearchKnowledge(ctx, KnowledgeSearchRequest{
		Scope: Scope{
			TenantID:       "tenant-a",
			AppID:          "app-a",
			InternalUserID: "internal-user-a",
			UserIDHash:     " hash-a ",
			Namespace:      "tenant/tenant-a",
		},
		Request: &knowledge.SearchRequest{Query: "deployment"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), MetadataUserIDHash)
}

func TestServiceSearchKnowledgeInjectsTenantAndInternalUserFilters(t *testing.T) {
	ctx := context.Background()
	knowledgeBackend := &capturingKnowledge{}
	service, err := New(ServiceConfig{
		Memory:    memoryinmemory.NewMemoryService(),
		Knowledge: knowledgeBackend,
	})
	require.NoError(t, err)
	scope := Scope{
		TenantID:       "tenant-a",
		AppID:          "app-a",
		InternalUserID: "internal-user-a",
		UserIDHash:     "hash-a",
		Namespace:      "tenant/tenant-a",
	}
	req := &knowledge.SearchRequest{
		Query: "deployment runbook",
		SearchFilter: &knowledge.SearchFilter{
			Metadata: map[string]any{"category": "runbook"},
		},
	}

	_, err = service.SearchKnowledge(ctx, KnowledgeSearchRequest{
		Scope:   scope,
		Request: req,
	})
	require.NoError(t, err)

	assert.Equal(t, "internal-user-a", knowledgeBackend.last.UserID)
	assert.Equal(t, "tenant-a", knowledgeBackend.last.SearchFilter.Metadata[MetadataTenantID])
	assert.Equal(t, "app-a", knowledgeBackend.last.SearchFilter.Metadata[MetadataAppID])
	assert.Equal(t, "internal-user-a", knowledgeBackend.last.SearchFilter.Metadata[MetadataInternalUserID])
	assert.Equal(t, "hash-a", knowledgeBackend.last.SearchFilter.Metadata[MetadataUserIDHash])
	assert.Equal(t, "runbook", knowledgeBackend.last.SearchFilter.Metadata["category"])
	assert.NotContains(t, req.SearchFilter.Metadata, MetadataTenantID)
	assert.NotContains(t, req.SearchFilter.Metadata, MetadataInternalUserID)
}

func TestServiceRejectsKnowledgeFilterOutsideScope(t *testing.T) {
	ctx := context.Background()
	service, err := New(ServiceConfig{
		Memory:    memoryinmemory.NewMemoryService(),
		Knowledge: &capturingKnowledge{},
	})
	require.NoError(t, err)
	scope := Scope{
		TenantID:       "tenant-a",
		AppID:          "app-a",
		InternalUserID: "internal-user-a",
		UserIDHash:     "hash-a",
		Namespace:      "tenant/tenant-a",
	}

	tests := []struct {
		name string
		req  *knowledge.SearchRequest
	}{
		{
			name: "conflicting tenant metadata",
			req: &knowledge.SearchRequest{
				Query: "deployment",
				SearchFilter: &knowledge.SearchFilter{
					Metadata: map[string]any{MetadataTenantID: "tenant-b"},
				},
			},
		},
		{
			name: "conflicting internal user metadata",
			req: &knowledge.SearchRequest{
				Query: "deployment",
				SearchFilter: &knowledge.SearchFilter{
					Metadata: map[string]any{MetadataInternalUserID: "internal-user-b"},
				},
			},
		},
		{
			name: "conflicting knowledge user",
			req: &knowledge.SearchRequest{
				Query:  "deployment",
				UserID: "internal-user-b",
			},
		},
		{
			name: "non-string tenant metadata",
			req: &knowledge.SearchRequest{
				Query: "deployment",
				SearchFilter: &knowledge.SearchFilter{
					Metadata: map[string]any{MetadataTenantID: []string{"tenant-a"}},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := service.SearchKnowledge(ctx, KnowledgeSearchRequest{
				Scope:   scope,
				Request: tt.req,
			})

			require.ErrorIs(t, err, ErrFilterOutsideScope)
		})
	}
}

type capturingKnowledge struct {
	last knowledge.SearchRequest
}

func (c *capturingKnowledge) Search(
	ctx context.Context,
	req *knowledge.SearchRequest,
) (*knowledge.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.last = *req
	return &knowledge.SearchResult{}, nil
}
