//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package awaitreply

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
)

func TestTool_Declaration(t *testing.T) {
	tl := New()

	decl := tl.Declaration()
	require.NotNil(t, decl)
	require.Equal(t, ToolName, decl.Name)
	require.NotNil(t, decl.InputSchema)
	require.Equal(t, "object", decl.InputSchema.Type)
	require.Empty(t, decl.InputSchema.Properties)
}

func TestTool_CallMarksInvocation(t *testing.T) {
	tl := New()
	inv := &agent.Invocation{AgentName: "clarifier"}
	ctx := agent.NewInvocationContext(context.Background(), inv)

	got, err := tl.Call(ctx, []byte(`{}`))
	require.NoError(t, err)

	resp, ok := got.(Response)
	require.True(t, ok)
	require.True(t, resp.Success)
	require.Equal(t, "clarifier", resp.AgentName)

	route, ok := agent.CurrentAwaitUserReplyRoute(inv)
	require.True(t, ok)
	require.Equal(t, "clarifier", route.AgentName)
}

func TestTool_CallRedactsModelVisibleResponseFields(t *testing.T) {
	tl := New()
	inv := &agent.Invocation{
		AgentName: "clarifier Authorization: ApiKey raw-token",
	}
	ctx := agent.NewInvocationContext(context.Background(), inv)

	got, err := tl.Call(ctx, []byte(`{}`))
	require.NoError(t, err)

	resp, ok := got.(Response)
	require.True(t, ok)
	require.True(t, resp.Success)
	require.NotContains(t, resp.AgentName, "raw-token")
	require.Contains(t, resp.AgentName, "Authorization: ****")
}

func TestTool_CallWithoutInvocationContext(t *testing.T) {
	tl := New()

	got, err := tl.Call(context.Background(), []byte(`{}`))
	require.NoError(t, err)

	resp, ok := got.(Response)
	require.True(t, ok)
	require.False(t, resp.Success)
	require.Contains(t, resp.Message, "invocation context")
}

func TestTool_CallInvalidJSON(t *testing.T) {
	tl := New()
	inv := &agent.Invocation{AgentName: "clarifier"}
	ctx := agent.NewInvocationContext(context.Background(), inv)

	got, err := tl.Call(ctx, []byte(`{`))
	require.NoError(t, err)

	resp, ok := got.(Response)
	require.True(t, ok)
	require.False(t, resp.Success)
	require.Contains(t, resp.Message, "invalid request format")

	_, ok = agent.CurrentAwaitUserReplyRoute(inv)
	require.False(t, ok)
}

func TestTool_CallInvalidInvocation(t *testing.T) {
	tl := New()
	inv := &agent.Invocation{}
	ctx := agent.NewInvocationContext(context.Background(), inv)

	got, err := tl.Call(ctx, []byte(`{}`))
	require.NoError(t, err)

	resp, ok := got.(Response)
	require.True(t, ok)
	require.False(t, resp.Success)
	require.Contains(t, resp.Message, "non-empty agent target")
}

func TestRedactResponseMessageMasksSecrets(t *testing.T) {
	got := redactResponseMessage(
		"failed Authorization: ApiKey raw-token\napi_key=sk-1234567890abcdef\nCookie: session=abc; sid=def",
	)
	for _, leaked := range []string{
		"raw-token",
		"sk-1234567890abcdef",
		"session=abc",
		"sid=def",
	} {
		require.NotContains(t, got, leaked)
	}
	for _, want := range []string{
		"Authorization: ****",
		"api_key=****",
		"Cookie: ****",
	} {
		require.Contains(t, got, want)
	}
}
