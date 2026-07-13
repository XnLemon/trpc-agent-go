//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package gateway

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/platform"
)

func TestInMemoryRegistryAvoidsTenantAppDelimiterCollision(t *testing.T) {
	ctx := context.Background()
	registry := NewInMemoryRegistry()
	firstRunner := &recordingRunner{response: "first"}
	secondRunner := &recordingRunner{response: "second"}
	first := validRuntimeForBinding("tenant|app", "alpha", "binding", "wecom", "acct", firstRunner)
	second := validRuntimeForBinding("tenant", "app|alpha", "binding", "wecom", "acct", secondRunner)
	require.NoError(t, registry.Register(first))
	require.NoError(t, registry.Register(second))

	gotFirst, ok, err := registry.Lookup(ctx, inboundForRegistryRuntime(first))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Same(t, firstRunner, gotFirst.Runner)

	gotSecond, ok, err := registry.Lookup(ctx, inboundForRegistryRuntime(second))
	require.NoError(t, err)
	require.True(t, ok)
	assert.Same(t, secondRunner, gotSecond.Runner)
}

func inboundForRegistryRuntime(runtime Runtime) platform.InboundMessage {
	return platform.InboundMessage{
		TenantID:          runtime.Tenant.TenantID,
		AppID:             runtime.App.AppID,
		BindingID:         runtime.Binding.BindingID,
		Channel:           runtime.Binding.Channel,
		ChannelAccountID:  runtime.Binding.AccountID,
		PlatformMessageID: "msg-1",
		ExternalUserID:    "user-1",
		ConversationType:  platform.ConversationTypeDM,
		MessageType:       platform.MessageTypeText,
		ContentParts:      nil,
	}
}
