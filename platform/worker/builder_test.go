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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/artifact"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/memory"
	memoryinmemory "trpc.group/trpc-go/trpc-agent-go/memory/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/platform/artifactstore"
	"trpc.group/trpc-go/trpc-agent-go/platform/gateway"
	"trpc.group/trpc-go/trpc-agent-go/platform/storagerouter"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessioninmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

func TestRuntimeBuilderClosesGatewayStorageLoop(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-a"
	appID := "support-app"
	profileID := "storage-a"
	namespace := "tenant/" + tenantID + "/profile/" + profileID
	backendID := "backend-a"

	sessionService := sessioninmemory.NewSessionService()
	t.Cleanup(func() {
		require.NoError(t, sessionService.Close())
	})
	memoryService := memoryinmemory.NewMemoryService()
	t.Cleanup(func() {
		require.NoError(t, memoryService.Close())
	})
	metadataStore := artifactstore.NewInMemoryMetadataStore()
	objectStore := artifactstore.NewInMemoryObjectStore()
	artifactService, err := artifactstore.New(artifactstore.ServiceConfig{
		TenantID:      tenantID,
		Namespace:     namespace,
		MetadataStore: metadataStore,
		ObjectStore:   objectStore,
		MaxAttempts:   2,
	})
	require.NoError(t, err)
	knowledgeService := &stubKnowledge{}
	auditSink := platform.NewInMemoryAuditSink()

	router := storagerouter.NewInMemoryRouter()
	require.NoError(t, router.RegisterBackend(storagerouter.BackendSet{
		TenantID:  tenantID,
		BackendID: backendID,
		Session:   sessionService,
		Summary:   sessionService,
		Memory:    memoryService,
		Artifact:  artifactService,
		Knowledge: knowledgeService,
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

	tenant := platform.Tenant{
		TenantID: tenantID,
		Status:   platform.TenantStatusActive,
	}
	app := platform.AgentApp{
		TenantID:         tenantID,
		AppID:            appID,
		AppName:          "support",
		AgentName:        "storage-probe",
		StorageProfileID: profileID,
		Status:           platform.AppStatusActive,
	}
	binding := platform.ChannelBinding{
		TenantID:      tenantID,
		AppID:         appID,
		BindingID:     "binding-a",
		Channel:       "wecom",
		AccountID:     "account-a",
		WebhookPath:   "/channels/wecom/binding-a/callback",
		TokenRef:      "secret://channel/token-a",
		SecretRef:     "secret://channel/secret-a",
		Status:        platform.BindingStatusActive,
		ChannelLimits: platform.ChannelLimits{MaxTextLength: 4096},
	}

	var captured AgentDependencies
	builder, err := NewRuntimeBuilder(
		router,
		AgentFactoryFunc(func(
			_ context.Context,
			dependencies AgentDependencies,
		) (agent.Agent, error) {
			captured = dependencies
			return &storageProbeAgent{name: app.AgentName}, nil
		}),
	)
	require.NoError(t, err)
	runtime, err := builder.Build(ctx, tenant, app, binding)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, runtime.Runner.Close())
	})

	registry := gateway.NewInMemoryRegistry()
	require.NoError(t, registry.Register(runtime))
	service := gateway.NewService(
		registry,
		platform.NewInMemoryIdempotencyStore(),
		gateway.NewInMemoryOutboundStore(),
	)
	inbound := platform.InboundMessage{
		TenantID:          tenantID,
		AppID:             appID,
		BindingID:         binding.BindingID,
		Channel:           binding.Channel,
		ChannelAccountID:  binding.AccountID,
		PlatformMessageID: "message-a",
		ExternalUserID:    "external-user",
		ConversationType:  platform.ConversationTypeDM,
		MessageType:       platform.MessageTypeText,
		ContentParts: []platform.ContentPart{
			{Type: platform.ContentPartTypeText, Text: "persist this"},
		},
		ReceivedAt: time.Unix(100, 0),
	}
	result, err := service.HandleInbound(ctx, inbound)
	require.NoError(t, err)
	assert.Equal(t, "stored", result.Outbound.Content)
	auditRecords := auditSink.Records()
	require.Len(t, auditRecords, 1)
	assert.Equal(t, tenantID, auditRecords[0].TenantID)
	assert.Equal(t, "completed", auditRecords[0].Decision)

	internalUserID := platform.InternalUserID(
		tenantID,
		binding.Channel,
		inbound.ExternalUserID,
	)
	scopedAppName := namespace + "/" + app.AppID
	storedSession, err := sessionService.GetSession(ctx, session.Key{
		AppName:   scopedAppName,
		UserID:    internalUserID,
		SessionID: result.SessionID,
	})
	require.NoError(t, err)
	require.NotNil(t, storedSession)
	assert.NotEmpty(t, storedSession.Events)

	memories, err := memoryService.ReadMemories(ctx, memory.UserKey{
		AppName: scopedAppName,
		UserID:  internalUserID,
	}, 10)
	require.NoError(t, err)
	require.Len(t, memories, 1)
	require.NotNil(t, memories[0].Memory)
	assert.Equal(t, "persist this", memories[0].Memory.Memory)

	loadedArtifact, err := artifactService.LoadArtifact(ctx, artifact.SessionInfo{
		AppName:   scopedAppName,
		UserID:    internalUserID,
		SessionID: result.SessionID,
	}, "result.txt", nil)
	require.NoError(t, err)
	require.NotNil(t, loadedArtifact)
	assert.Equal(t, []byte("persist this"), loadedArtifact.Data)

	assert.Equal(t, tenantID, captured.Storage.Scope().TenantID)
	assert.Equal(t, profileID, captured.Storage.Scope().ProfileID)
	require.NotNil(t, captured.Session)
	require.NotNil(t, captured.Memory)
	require.NotNil(t, captured.Artifact)
	require.NotNil(t, captured.Knowledge)
	require.NotNil(t, captured.Audit)

	_, err = captured.Session.CreateSession(ctx, session.Key{
		AppName:   app.AppName,
		UserID:    internalUserID,
		SessionID: "unscoped-session",
	}, nil)
	assert.ErrorIs(t, err, storagerouter.ErrKeyOutsideTenantScope)

	err = captured.Memory.AddMemory(ctx, memory.UserKey{
		AppName: app.AppName,
		UserID:  internalUserID,
	}, "unscoped", nil)
	assert.ErrorIs(t, err, storagerouter.ErrKeyOutsideTenantScope)

	_, err = captured.Artifact.LoadArtifact(ctx, artifact.SessionInfo{
		AppName:   app.AppName,
		UserID:    internalUserID,
		SessionID: result.SessionID,
	}, "result.txt", nil)
	assert.ErrorIs(t, err, storagerouter.ErrKeyOutsideTenantScope)

	_, err = captured.Knowledge.Search(ctx, &knowledge.SearchRequest{
		Query: "deployment",
	})
	require.NoError(t, err)
	require.NotNil(t, knowledgeService.last)
	assert.Equal(
		t,
		tenantID,
		knowledgeService.last.SearchFilter.Metadata["tenant_id"],
	)

	err = captured.Audit.WriteAudit(ctx, platform.AuditRecord{
		TenantID: "tenant-b",
	})
	assert.ErrorIs(t, err, storagerouter.ErrKeyOutsideTenantScope)
}

func TestRuntimeBuilderRejectsIdentityMismatch(t *testing.T) {
	router := storagerouter.NewInMemoryRouter()
	builder, err := NewRuntimeBuilder(
		router,
		AgentFactoryFunc(func(
			context.Context,
			AgentDependencies,
		) (agent.Agent, error) {
			return &storageProbeAgent{name: "wrong-agent"}, nil
		}),
	)
	require.NoError(t, err)

	tenant := platform.Tenant{
		TenantID: "tenant-a",
		Status:   platform.TenantStatusActive,
	}
	app := platform.AgentApp{
		TenantID:         tenant.TenantID,
		AppID:            "app-a",
		AppName:          "app",
		AgentName:        "expected-agent",
		StorageProfileID: "profile-a",
		Status:           platform.AppStatusActive,
	}
	binding := platform.ChannelBinding{
		TenantID:    "tenant-b",
		AppID:       app.AppID,
		BindingID:   "binding-a",
		Channel:     "wecom",
		AccountID:   "account-a",
		WebhookPath: "/callback",
		TokenRef:    "secret://token",
		SecretRef:   "secret://secret",
		Status:      platform.BindingStatusActive,
	}

	_, err = builder.Build(context.Background(), tenant, app, binding)
	assert.ErrorIs(t, err, ErrRuntimeIdentityMismatch)
}

func TestRuntimeBuilderRejectsInactiveConfigBeforeFactory(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*platform.Tenant, *platform.AgentApp, *platform.ChannelBinding)
	}{
		{
			name: "tenant suspended",
			mutate: func(
				tenant *platform.Tenant,
				_ *platform.AgentApp,
				_ *platform.ChannelBinding,
			) {
				tenant.Status = platform.TenantStatusSuspended
			},
		},
		{
			name: "app suspended",
			mutate: func(
				_ *platform.Tenant,
				app *platform.AgentApp,
				_ *platform.ChannelBinding,
			) {
				app.Status = platform.AppStatusSuspended
			},
		},
		{
			name: "binding disabled",
			mutate: func(
				_ *platform.Tenant,
				_ *platform.AgentApp,
				binding *platform.ChannelBinding,
			) {
				binding.Status = platform.BindingStatusDisabled
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factoryCalled := false
			router := &countingRouter{
				Router: storagerouter.NewInMemoryRouter(),
			}
			builder, err := NewRuntimeBuilder(
				router,
				AgentFactoryFunc(func(
					context.Context,
					AgentDependencies,
				) (agent.Agent, error) {
					factoryCalled = true
					return &storageProbeAgent{name: "agent"}, nil
				}),
			)
			require.NoError(t, err)

			tenant := platform.Tenant{
				TenantID: "tenant-a",
				Status:   platform.TenantStatusActive,
			}
			app := platform.AgentApp{
				TenantID:         tenant.TenantID,
				AppID:            "app-a",
				AppName:          "app",
				AgentName:        "agent",
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
			tt.mutate(&tenant, &app, &binding)

			_, err = builder.Build(context.Background(), tenant, app, binding)
			assert.ErrorIs(t, err, gateway.ErrRuntimeInactive)
			assert.Zero(t, router.adapterCalls)
			assert.False(t, factoryCalled)
		})
	}
}

func TestNewRuntimeBuilderRejectsTypedNilFactory(t *testing.T) {
	var factory AgentFactoryFunc

	_, err := NewRuntimeBuilder(storagerouter.NewInMemoryRouter(), factory)

	assert.ErrorIs(t, err, ErrAgentFactoryRequired)
}

func TestNewRuntimeBuilderRejectsTypedNilRouter(t *testing.T) {
	var router *storagerouter.InMemoryRouter

	_, err := NewRuntimeBuilder(
		router,
		AgentFactoryFunc(func(
			context.Context,
			AgentDependencies,
		) (agent.Agent, error) {
			return &storageProbeAgent{name: "agent"}, nil
		}),
	)

	assert.ErrorIs(t, err, ErrStorageRouterRequired)
}

func TestRuntimeBuilderRejectsInvalidAgent(t *testing.T) {
	ctx := context.Background()
	tenantID := "tenant-a"
	profileID := "profile-a"
	backendID := "backend-a"
	namespace := "tenant/" + tenantID + "/profile/" + profileID

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

	router := storagerouter.NewInMemoryRouter()
	require.NoError(t, router.RegisterBackend(storagerouter.BackendSet{
		TenantID:  tenantID,
		BackendID: backendID,
		Session:   sessionService,
		Summary:   sessionService,
		Memory:    memoryService,
		Artifact:  artifactService,
		Knowledge: &stubKnowledge{},
		Audit:     platform.NewInMemoryAuditSink(),
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

	tenant := platform.Tenant{
		TenantID: tenantID,
		Status:   platform.TenantStatusActive,
	}
	app := platform.AgentApp{
		TenantID:         tenantID,
		AppID:            "app-a",
		AppName:          "app",
		AgentName:        "expected-agent",
		StorageProfileID: profileID,
		Status:           platform.AppStatusActive,
	}
	binding := platform.ChannelBinding{
		TenantID:    tenantID,
		AppID:       app.AppID,
		BindingID:   "binding-a",
		Channel:     "wecom",
		AccountID:   "account-a",
		WebhookPath: "/callback",
		TokenRef:    "secret://token",
		SecretRef:   "secret://secret",
		Status:      platform.BindingStatusActive,
	}

	t.Run("name mismatch", func(t *testing.T) {
		builder, err := NewRuntimeBuilder(
			router,
			AgentFactoryFunc(func(
				context.Context,
				AgentDependencies,
			) (agent.Agent, error) {
				return &storageProbeAgent{name: "wrong-agent"}, nil
			}),
		)
		require.NoError(t, err)

		_, err = builder.Build(ctx, tenant, app, binding)
		assert.ErrorIs(t, err, ErrAgentNameMismatch)
	})

	t.Run("typed nil", func(t *testing.T) {
		var nilAgent *storageProbeAgent
		builder, err := NewRuntimeBuilder(
			router,
			AgentFactoryFunc(func(
				context.Context,
				AgentDependencies,
			) (agent.Agent, error) {
				return nilAgent, nil
			}),
		)
		require.NoError(t, err)

		_, err = builder.Build(ctx, tenant, app, binding)
		assert.ErrorIs(t, err, ErrAgentRequired)
	})
}

func TestRuntimeBuilderRejectsNilStorageDependencies(t *testing.T) {
	ctx := context.Background()
	tenant := platform.Tenant{
		TenantID: "tenant-a",
		Status:   platform.TenantStatusActive,
	}
	app := platform.AgentApp{
		TenantID:         tenant.TenantID,
		AppID:            "app-a",
		AppName:          "app",
		AgentName:        "agent",
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

	tests := []struct {
		name          string
		adapter       storagerouter.StorageAdapter
		wantErr       error
		wantFactory   bool
		mutateAdapter func(*nilStorageAdapter)
	}{
		{
			name:    "nil adapter",
			adapter: nil,
			wantErr: ErrStorageAdapterRequired,
		},
		{
			name:    "typed nil adapter",
			adapter: (*nilStorageAdapter)(nil),
			wantErr: ErrStorageAdapterRequired,
		},
		{
			name: "nil session",
			mutateAdapter: func(a *nilStorageAdapter) {
				a.session = nil
			},
			wantErr: ErrSessionServiceRequired,
		},
		{
			name: "typed nil session",
			mutateAdapter: func(a *nilStorageAdapter) {
				var sessionService *sessioninmemory.SessionService
				a.session = sessionService
			},
			wantErr: ErrSessionServiceRequired,
		},
		{
			name: "nil memory",
			mutateAdapter: func(a *nilStorageAdapter) {
				a.memory = nil
			},
			wantErr: ErrMemoryServiceRequired,
		},
		{
			name: "typed nil memory",
			mutateAdapter: func(a *nilStorageAdapter) {
				var memoryService *memoryinmemory.MemoryService
				a.memory = memoryService
			},
			wantErr: ErrMemoryServiceRequired,
		},
		{
			name: "nil artifact",
			mutateAdapter: func(a *nilStorageAdapter) {
				a.artifact = nil
			},
			wantErr: ErrArtifactServiceRequired,
		},
		{
			name: "typed nil artifact",
			mutateAdapter: func(a *nilStorageAdapter) {
				var artifactService *artifactstore.Service
				a.artifact = artifactService
			},
			wantErr: ErrArtifactServiceRequired,
		},
		{
			name: "nil knowledge",
			mutateAdapter: func(a *nilStorageAdapter) {
				a.knowledge = nil
			},
			wantErr: ErrKnowledgeServiceRequired,
		},
		{
			name: "typed nil knowledge",
			mutateAdapter: func(a *nilStorageAdapter) {
				var knowledgeService *stubKnowledge
				a.knowledge = knowledgeService
			},
			wantErr: ErrKnowledgeServiceRequired,
		},
		{
			name: "nil audit",
			mutateAdapter: func(a *nilStorageAdapter) {
				a.audit = nil
			},
			wantErr: ErrAuditSinkRequired,
		},
		{
			name: "typed nil audit",
			mutateAdapter: func(a *nilStorageAdapter) {
				var auditSink *platform.InMemoryAuditSink
				a.audit = auditSink
			},
			wantErr: ErrAuditSinkRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := tt.adapter
			if adapter == nil && tt.name != "nil adapter" {
				baseAdapter, cleanup := newNilStorageAdapter(t)
				defer cleanup()
				if tt.mutateAdapter != nil {
					tt.mutateAdapter(baseAdapter)
				}
				adapter = baseAdapter
			}
			factoryCalled := false
			builder, err := NewRuntimeBuilder(
				stubAdapterRouter{adapter: adapter},
				AgentFactoryFunc(func(
					context.Context,
					AgentDependencies,
				) (agent.Agent, error) {
					factoryCalled = true
					return &storageProbeAgent{name: app.AgentName}, nil
				}),
			)
			require.NoError(t, err)

			_, err = builder.Build(ctx, tenant, app, binding)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.False(t, factoryCalled)
		})
	}
}

type storageProbeAgent struct {
	name string
}

func (a *storageProbeAgent) Run(
	ctx context.Context,
	invocation *agent.Invocation,
) (<-chan *event.Event, error) {
	userKey := memory.UserKey{
		AppName: invocation.Session.AppName,
		UserID:  invocation.Session.UserID,
	}
	if err := invocation.MemoryService.AddMemory(
		ctx,
		userKey,
		invocation.Message.Content,
		[]string{"gateway"},
	); err != nil {
		return nil, err
	}
	if _, err := invocation.ArtifactService.SaveArtifact(
		ctx,
		artifact.SessionInfo{
			AppName:   invocation.Session.AppName,
			UserID:    invocation.Session.UserID,
			SessionID: invocation.Session.ID,
		},
		"result.txt",
		&artifact.Artifact{
			Data:     []byte(invocation.Message.Content),
			MimeType: "text/plain",
			Name:     "result.txt",
		},
	); err != nil {
		return nil, err
	}

	out := make(chan *event.Event, 1)
	out <- event.NewResponseEvent(
		invocation.InvocationID,
		a.name,
		&model.Response{
			ID:     "storage-probe-response",
			Object: model.ObjectTypeChatCompletion,
			Done:   true,
			Choices: []model.Choice{
				{
					Index: 0,
					Message: model.Message{
						Role:    model.RoleAssistant,
						Content: "stored",
					},
				},
			},
		},
	)
	close(out)
	return out, nil
}

func (a *storageProbeAgent) Tools() []tool.Tool {
	return nil
}

func (a *storageProbeAgent) Info() agent.Info {
	return agent.Info{Name: a.name}
}

func (a *storageProbeAgent) SubAgents() []agent.Agent {
	return nil
}

func (a *storageProbeAgent) FindSubAgent(string) agent.Agent {
	return nil
}

type stubKnowledge struct {
	last *knowledge.SearchRequest
}

func (s *stubKnowledge) Search(
	ctx context.Context,
	req *knowledge.SearchRequest,
) (*knowledge.SearchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req != nil {
		copied := *req
		s.last = &copied
	}
	return &knowledge.SearchResult{}, nil
}

type countingRouter struct {
	storagerouter.Router
	adapterCalls int
}

func (r *countingRouter) Adapter(
	ctx context.Context,
	tenantID string,
	profileID string,
) (storagerouter.StorageAdapter, error) {
	r.adapterCalls++
	adapterRouter, ok := r.Router.(storagerouter.AdapterRouter)
	if !ok {
		return nil, errors.New("adapter router not implemented")
	}
	return adapterRouter.Adapter(ctx, tenantID, profileID)
}

type stubAdapterRouter struct {
	adapter storagerouter.StorageAdapter
	err     error
}

func (r stubAdapterRouter) Profile(
	context.Context,
	string,
	string,
) (platform.StorageProfile, error) {
	return platform.StorageProfile{}, errors.New("not implemented")
}

func (r stubAdapterRouter) Adapter(
	ctx context.Context,
	tenantID string,
	profileID string,
) (storagerouter.StorageAdapter, error) {
	_, _ = tenantID, profileID
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.adapter, r.err
}

func (r stubAdapterRouter) Route(
	context.Context,
	string,
	string,
	platform.BackendMigrationResource,
) (storagerouter.RouteBinding, error) {
	return storagerouter.RouteBinding{}, errors.New("not implemented")
}

func (r stubAdapterRouter) Status(
	context.Context,
	string,
	string,
) (storagerouter.StatusSummary, error) {
	return storagerouter.StatusSummary{}, errors.New("not implemented")
}

func (r stubAdapterRouter) Session(context.Context, string, string) (session.Service, error) {
	return nil, errors.New("not implemented")
}

func (r stubAdapterRouter) Summary(context.Context, string, string) (storagerouter.SummaryStore, error) {
	return nil, errors.New("not implemented")
}

func (r stubAdapterRouter) Memory(context.Context, string, string) (memory.Service, error) {
	return nil, errors.New("not implemented")
}

func (r stubAdapterRouter) Artifact(context.Context, string, string) (artifact.Service, error) {
	return nil, errors.New("not implemented")
}

func (r stubAdapterRouter) Knowledge(context.Context, string, string) (knowledge.Knowledge, error) {
	return nil, errors.New("not implemented")
}

func (r stubAdapterRouter) Audit(context.Context, string, string) (platform.AuditSink, error) {
	return nil, errors.New("not implemented")
}

type nilStorageAdapter struct {
	scope     storagerouter.StorageScope
	session   session.Service
	memory    memory.Service
	artifact  artifact.Service
	knowledge knowledge.Knowledge
	audit     platform.AuditSink
}

func newNilStorageAdapter(t *testing.T) (*nilStorageAdapter, func()) {
	t.Helper()
	sessionService := sessioninmemory.NewSessionService()
	memoryService := memoryinmemory.NewMemoryService()
	artifactService, err := artifactstore.New(artifactstore.ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a/profile/profile-a",
		MetadataStore: artifactstore.NewInMemoryMetadataStore(),
		ObjectStore:   artifactstore.NewInMemoryObjectStore(),
		MaxAttempts:   2,
	})
	require.NoError(t, err)
	return &nilStorageAdapter{
			scope: storagerouter.StorageScope{
				TenantID:  "tenant-a",
				ProfileID: "profile-a",
				Namespace: "tenant/tenant-a/profile/profile-a",
			},
			session:   sessionService,
			memory:    memoryService,
			artifact:  artifactService,
			knowledge: &stubKnowledge{},
			audit:     platform.NewInMemoryAuditSink(),
		}, func() {
			require.NoError(t, sessionService.Close())
			require.NoError(t, memoryService.Close())
		}
}

func (a *nilStorageAdapter) Scope() storagerouter.StorageScope {
	return a.scope
}

func (a *nilStorageAdapter) Route(
	context.Context,
	platform.BackendMigrationResource,
) (storagerouter.RouteBinding, error) {
	return storagerouter.RouteBinding{}, errors.New("not implemented")
}

func (a *nilStorageAdapter) Session(context.Context) (session.Service, error) {
	return a.session, nil
}

func (a *nilStorageAdapter) Summary(context.Context) (storagerouter.SummaryStore, error) {
	return a.session.(storagerouter.SummaryStore), nil
}

func (a *nilStorageAdapter) Memory(context.Context) (memory.Service, error) {
	return a.memory, nil
}

func (a *nilStorageAdapter) Artifact(context.Context) (artifact.Service, error) {
	return a.artifact, nil
}

func (a *nilStorageAdapter) Knowledge(context.Context) (knowledge.Knowledge, error) {
	return a.knowledge, nil
}

func (a *nilStorageAdapter) Audit(context.Context) (platform.AuditSink, error) {
	return a.audit, nil
}
