//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package worker

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	memoryinmemory "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/platform/artifactstore"
	"trpc.group/trpc-go/trpc-agent-go/platform/gateway"
	"trpc.group/trpc-go/trpc-agent-go/platform/storagerouter"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestRuntimeBuilderAppliesToolGovernanceEndToEnd(t *testing.T) {
	ctx := context.Background()
	router, auditSink := governanceTestRouter(t)
	tenant, app, binding := governanceRuntimeConfig()
	app.ToolPolicyID = "policy-a"
	policy := platform.ToolPolicy{
		TenantID:            tenant.TenantID,
		AppID:               app.AppID,
		PolicyID:            app.ToolPolicyID,
		ToolWhitelist:       []string{"read_file", "shell"},
		ToolDenylist:        []string{"shell"},
		HighRiskTools:       []string{"shell"},
		DangerousToolAction: platform.DangerousToolActionDeny,
	}
	providerCalled := false
	provider := ToolPolicyProviderFunc(func(
		_ context.Context,
		tenantID string,
		appID string,
		policyID string,
	) (platform.ToolPolicy, error) {
		providerCalled = true
		assert.Equal(t, tenant.TenantID, tenantID)
		assert.Equal(t, app.AppID, appID)
		assert.Equal(t, app.ToolPolicyID, policyID)
		return policy, nil
	})
	var captured AgentDependencies
	builder, err := NewRuntimeBuilder(
		router,
		AgentFactoryFunc(func(
			_ context.Context,
			dependencies AgentDependencies,
		) (agent.Agent, error) {
			captured = dependencies
			return newGovernanceProbeAgent(app.AgentName), nil
		}),
		WithToolPolicyProvider(provider),
	)
	require.NoError(t, err)
	runtime, err := builder.Build(ctx, tenant, app, binding)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, runtime.Runner.Close())
	})

	assert.True(t, providerCalled)
	assert.Equal(t, policy, captured.ToolPolicy)
	require.NotNil(t, captured.ToolFilter)
	require.NotNil(t, captured.ToolPermissionPolicy)
	require.NotNil(t, runtime.ToolFilter)
	require.NotNil(t, runtime.ToolPermissionPolicy)

	registry := gateway.NewInMemoryRegistry()
	require.NoError(t, registry.Register(runtime))
	service := gateway.NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		gateway.NewInMemoryOutboundStore(),
	)
	result, err := service.HandleInbound(ctx, governanceInbound(tenant, app, binding))
	require.NoError(t, err)
	assert.Equal(t, "visible=read_file permission=deny", result.Outbound.Content)

	records := auditSink.Records()
	require.Len(t, records, 2)
	assert.Equal(t, "shell", records[0].ToolName)
	assert.Equal(t, string(tool.PermissionActionDeny), records[0].Decision)
	assert.Equal(t, tenant.TenantID, records[0].TenantID)
	assert.Equal(t, app.AppID, records[0].AppID)
	assert.Equal(t, "completed", records[1].Decision)
}

func TestRuntimeBuilderRejectsMissingToolPolicyProviderBeforeStorage(t *testing.T) {
	tenant, app, binding := governanceRuntimeConfig()
	app.ToolPolicyID = "policy-a"
	router := &countingRouter{Router: storagerouter.NewInMemoryRouter()}
	builder, err := NewRuntimeBuilder(
		router,
		AgentFactoryFunc(func(
			context.Context,
			AgentDependencies,
		) (agent.Agent, error) {
			return newGovernanceProbeAgent(app.AgentName), nil
		}),
	)
	require.NoError(t, err)

	_, err = builder.Build(context.Background(), tenant, app, binding)
	assert.ErrorIs(t, err, ErrToolPolicyProviderRequired)
	assert.Zero(t, router.adapterCalls)
}

func TestRuntimeBuilderRejectsMismatchedToolPolicyBeforeStorage(t *testing.T) {
	tenant, app, binding := governanceRuntimeConfig()
	app.ToolPolicyID = "policy-a"
	router := &countingRouter{Router: storagerouter.NewInMemoryRouter()}
	builder, err := NewRuntimeBuilder(
		router,
		AgentFactoryFunc(func(
			context.Context,
			AgentDependencies,
		) (agent.Agent, error) {
			return newGovernanceProbeAgent(app.AgentName), nil
		}),
		WithToolPolicyProvider(ToolPolicyProviderFunc(func(
			context.Context,
			string,
			string,
			string,
		) (platform.ToolPolicy, error) {
			return platform.ToolPolicy{
				TenantID: "tenant-b",
				AppID:    app.AppID,
				PolicyID: app.ToolPolicyID,
			}, nil
		})),
	)
	require.NoError(t, err)

	_, err = builder.Build(context.Background(), tenant, app, binding)
	assert.ErrorIs(t, err, ErrToolPolicyIdentityMismatch)
	assert.Zero(t, router.adapterCalls)
}

func TestRuntimeBuilderIsolatesResolvedAndCompiledToolPolicySlices(
	t *testing.T,
) {
	ctx := context.Background()
	router, _ := governanceTestRouter(t)
	tenant, app, binding := governanceRuntimeConfig()
	app.ToolPolicyID = "policy-a"
	source := platform.ToolPolicy{
		TenantID:               tenant.TenantID,
		AppID:                  app.AppID,
		PolicyID:               app.ToolPolicyID,
		ToolWhitelist:          []string{"read_file"},
		ToolDenylist:           []string{"shell"},
		ArgumentRedactionRules: []string{"secret"},
		PlatformDenylist:       []string{"admin"},
		HighRiskTools:          []string{"shell"},
		DangerousToolAction:    platform.DangerousToolActionDeny,
	}
	builder, err := NewRuntimeBuilder(
		router,
		AgentFactoryFunc(func(
			_ context.Context,
			dependencies AgentDependencies,
		) (agent.Agent, error) {
			source.ToolWhitelist[0] = "provider_mutation"
			source.ToolDenylist[0] = "provider_mutation"
			source.ArgumentRedactionRules[0] = "provider_mutation"
			source.PlatformDenylist[0] = "provider_mutation"
			source.HighRiskTools[0] = "provider_mutation"
			require.Equal(t, "read_file", dependencies.ToolPolicy.ToolWhitelist[0])
			require.Equal(t, "shell", dependencies.ToolPolicy.ToolDenylist[0])
			require.Equal(t, "secret", dependencies.ToolPolicy.ArgumentRedactionRules[0])
			require.Equal(t, "admin", dependencies.ToolPolicy.PlatformDenylist[0])
			require.Equal(t, "shell", dependencies.ToolPolicy.HighRiskTools[0])

			dependencies.ToolPolicy.ToolWhitelist[0] = "factory_mutation"
			dependencies.ToolPolicy.ToolDenylist[0] = "factory_mutation"
			return newGovernanceProbeAgent(app.AgentName), nil
		}),
		WithToolPolicyProvider(ToolPolicyProviderFunc(func(
			context.Context,
			string,
			string,
			string,
		) (platform.ToolPolicy, error) {
			return source, nil
		})),
	)
	require.NoError(t, err)

	runtime, err := builder.Build(ctx, tenant, app, binding)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, runtime.Runner.Close())
	})
	require.True(t, runtime.ToolFilter(
		ctx,
		&governanceProbeTool{name: "read_file"},
	))
	require.False(t, runtime.ToolFilter(
		ctx,
		&governanceProbeTool{name: "shell"},
	))
	decision, err := runtime.ToolPermissionPolicy.CheckToolPermission(
		ctx,
		&tool.PermissionRequest{ToolName: "shell"},
	)
	require.NoError(t, err)
	require.Equal(t, tool.PermissionActionDeny, decision.Action)
}

func governanceTestRouter(
	t *testing.T,
) (*storagerouter.InMemoryRouter, *platform.InMemoryAuditSink) {
	t.Helper()
	tenantID := "tenant-a"
	profileID := "profile-a"
	backendID := "backend-a"
	namespace := "tenant/" + tenantID
	sessionService := sessioninmemory.NewSessionService()
	t.Cleanup(func() {
		require.NoError(t, sessionService.Close())
	})
	memoryService := memoryinmemory.NewMemoryService()
	t.Cleanup(func() {
		require.NoError(t, memoryService.Close())
	})
	artifactService, err := artifactstore.New(artifactstore.ServiceConfig{
		TenantID:      tenantID,
		Namespace:     namespace,
		MetadataStore: artifactstore.NewInMemoryMetadataStore(),
		ObjectStore:   artifactstore.NewInMemoryObjectStore(),
		MaxAttempts:   2,
	})
	require.NoError(t, err)
	auditSink := platform.NewInMemoryAuditSink()
	router := storagerouter.NewInMemoryRouter()
	require.NoError(t, router.RegisterBackend(storagerouter.BackendSet{
		TenantID:  tenantID,
		BackendID: backendID,
		Session:   sessionService,
		Summary:   sessionService,
		Memory:    memoryService,
		Artifact:  artifactService,
		Knowledge: &stubKnowledge{},
		Audit:     auditSink,
	}))
	require.NoError(t, router.RegisterProfile(platform.StorageProfile{
		TenantID:         tenantID,
		ProfileID:        profileID,
		SessionBackend:   backendID,
		MemoryBackend:    backendID,
		SummaryBackend:   backendID,
		ArtifactBackend:  backendID,
		KnowledgeBackend: backendID,
		AuditBackend:     backendID,
		DSNRef:           "secret://storage/" + tenantID,
		Namespace:        namespace,
	}))
	return router, auditSink
}

func governanceRuntimeConfig() (
	platform.Tenant,
	platform.AgentApp,
	platform.ChannelBinding,
) {
	tenant := platform.Tenant{
		TenantID: "tenant-a",
		Status:   platform.TenantStatusActive,
	}
	app := platform.AgentApp{
		TenantID:         tenant.TenantID,
		AppID:            "app-a",
		AppName:          "app",
		AgentName:        "governance-probe",
		StorageProfileID: "profile-a",
		Status:           platform.AppStatusActive,
	}
	binding := platform.ChannelBinding{
		TenantID:    tenant.TenantID,
		AppID:       app.AppID,
		BindingID:   "binding-a",
		Channel:     "wecom",
		AccountID:   "account-a",
		WebhookPath: "/callback",
		TokenRef:    "secret://token",
		SecretRef:   "secret://secret",
		Status:      platform.BindingStatusActive,
	}
	return tenant, app, binding
}

func governanceInbound(
	tenant platform.Tenant,
	app platform.AgentApp,
	binding platform.ChannelBinding,
) platform.InboundMessage {
	return platform.InboundMessage{
		TenantID:          tenant.TenantID,
		AppID:             app.AppID,
		BindingID:         binding.BindingID,
		Channel:           binding.Channel,
		ChannelAccountID:  binding.AccountID,
		PlatformMessageID: "governance-message",
		ExternalUserID:    "external-user",
		ConversationType:  platform.ConversationTypeDM,
		MessageType:       platform.MessageTypeText,
		ContentParts: []platform.ContentPart{
			{Type: platform.ContentPartTypeText, Text: "check governance"},
		},
		ReceivedAt: time.Unix(100, 0),
	}
}

type governanceProbeAgent struct {
	name  string
	tools []tool.Tool
}

func newGovernanceProbeAgent(name string) *governanceProbeAgent {
	return &governanceProbeAgent{
		name: name,
		tools: []tool.Tool{
			&governanceProbeTool{name: "read_file"},
			&governanceProbeTool{name: "shell"},
		},
	}
}

func (a *governanceProbeAgent) Run(
	ctx context.Context,
	invocation *agent.Invocation,
) (<-chan *event.Event, error) {
	visible := tool.FilterTools(
		ctx,
		a.tools,
		invocation.RunOptions.MandatoryToolFilter,
	)
	visibleNames := make([]string, 0, len(visible))
	for _, candidate := range visible {
		visibleNames = append(visibleNames, candidate.Declaration().Name)
	}
	shell := a.tools[1]
	decision, err := invocation.RunOptions.CheckToolPermission(
		ctx,
		&tool.PermissionRequest{
			Tool:        shell,
			ToolName:    shell.Declaration().Name,
			ToolCallID:  "call-shell",
			Declaration: shell.Declaration(),
			Arguments:   []byte(`{"command":"restricted"}`),
		},
	)
	if err != nil {
		return nil, err
	}
	out := make(chan *event.Event, 1)
	out <- event.NewResponseEvent(
		invocation.InvocationID,
		a.name,
		&model.Response{
			ID:     "governance-probe-response",
			Object: model.ObjectTypeChatCompletion,
			Done:   true,
			Choices: []model.Choice{
				{
					Index: 0,
					Message: model.Message{
						Role: model.RoleAssistant,
						Content: "visible=" + strings.Join(visibleNames, ",") +
							" permission=" + string(decision.Action),
					},
				},
			},
		},
	)
	close(out)
	return out, nil
}

func (a *governanceProbeAgent) Tools() []tool.Tool {
	return a.tools
}

func (a *governanceProbeAgent) Info() agent.Info {
	return agent.Info{Name: a.name}
}

func (a *governanceProbeAgent) SubAgents() []agent.Agent {
	return nil
}

func (a *governanceProbeAgent) FindSubAgent(string) agent.Agent {
	return nil
}

type governanceProbeTool struct {
	name string
}

func (t *governanceProbeTool) Declaration() *tool.Declaration {
	return &tool.Declaration{Name: t.name}
}
