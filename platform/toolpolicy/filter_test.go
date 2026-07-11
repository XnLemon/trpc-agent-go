//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package toolpolicy

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestPolicyToolFilterMatchesNameGovernance(t *testing.T) {
	policy, err := New(platform.ToolPolicy{
		TenantID:         "tenant-a",
		AppID:            "app-a",
		PolicyID:         "policy-a",
		ToolWhitelist:    []string{"read_file", "workspace_write"},
		ToolDenylist:     []string{"workspace_write"},
		PlatformDenylist: []string{"shell"},
	})
	require.NoError(t, err)
	filter := policy.ToolFilter()
	require.NotNil(t, filter)

	assert.True(t, filter(context.Background(), namedTool("read_file")))
	assert.False(t, filter(context.Background(), namedTool("workspace_write")))
	assert.False(t, filter(context.Background(), namedTool("shell")))
	assert.False(t, filter(context.Background(), namedTool("unknown")))
	assert.False(t, filter(context.Background(), nil))
}

func TestPolicyToolFilterAllowsNonDeniedToolsWithoutWhitelist(t *testing.T) {
	policy, err := New(platform.ToolPolicy{
		TenantID:     "tenant-a",
		AppID:        "app-a",
		PolicyID:     "policy-a",
		ToolDenylist: []string{"blocked"},
	})
	require.NoError(t, err)
	filter := policy.ToolFilter()
	require.NotNil(t, filter)

	assert.True(t, filter(context.Background(), namedTool("allowed")))
	assert.False(t, filter(context.Background(), namedTool("blocked")))
}

func TestPolicyToolFilterReturnsNilWithoutNameConstraints(t *testing.T) {
	policy, err := New(platform.ToolPolicy{
		TenantID: "tenant-a",
		AppID:    "app-a",
		PolicyID: "policy-a",
	})
	require.NoError(t, err)

	assert.Nil(t, policy.ToolFilter())
}

type filterTestTool struct {
	declaration *tool.Declaration
}

func namedTool(name string) tool.Tool {
	return &filterTestTool{
		declaration: &tool.Declaration{Name: name},
	}
}

func (t *filterTestTool) Declaration() *tool.Declaration {
	return t.declaration
}
