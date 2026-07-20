//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package live_coordination_sample_test

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
	"trpc.group/trpc-go/trpc-a2a-go/taskmanager"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/a2aagent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	a2aserver "trpc.group/trpc-go/trpc-agent-go/server/a2a"
	"trpc.group/trpc-go/trpc-agent-go/session"
	sessionmemory "trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

type backendAgent struct{}

func (backendAgent) Info() agent.Info {
	return agent.Info{Name: "live-remote", Description: "live anonymous-cookie integration server"}
}

func (backendAgent) Tools() []tool.Tool { return nil }

func (backendAgent) Run(
	_ context.Context,
	invocation *agent.Invocation,
) (<-chan *event.Event, error) {
	result := make(chan *event.Event, 1)
	result <- &event.Event{
		InvocationID: invocation.InvocationID,
		Author:       "live-remote",
		ID:           "live-response",
		Timestamp:    time.Now(),
		Response: &model.Response{
			ID:      "live-response",
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   "live-backend",
			Done:    true,
			Choices: []model.Choice{{
				Message: model.Message{Role: model.RoleAssistant, Content: "live response"},
			}},
		},
	}
	close(result)
	return result, nil
}

func (backendAgent) SubAgents() []agent.Agent { return nil }

func (backendAgent) FindSubAgent(string) agent.Agent { return nil }

type recordingProcessor struct {
	next taskmanager.MessageProcessor

	mu     sync.Mutex
	userID []string
}

func (p *recordingProcessor) ProcessMessage(
	ctx context.Context,
	message protocol.Message,
	options taskmanager.ProcessOptions,
	taskHandler taskmanager.TaskHandler,
) (*taskmanager.MessageProcessingResult, error) {
	userID, _ := a2aserver.UserIDFromContext(ctx)
	p.mu.Lock()
	p.userID = append(p.userID, userID)
	p.mu.Unlock()
	return p.next.ProcessMessage(ctx, message, options, taskHandler)
}

func TestLiveAnonymousCookieCoordination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	host := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	remoteURL := "http://" + host

	recorder := &recordingProcessor{}
	remote, err := a2aserver.New(
		a2aserver.WithAgent(backendAgent{}, false),
		a2aserver.WithHost(host),
		a2aserver.WithProcessMessageHook(func(next taskmanager.MessageProcessor) taskmanager.MessageProcessor {
			recorder.next = next
			return recorder
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	serverErr := make(chan error, 1)
	go func() { serverErr <- remote.Start(host) }()
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := remote.Stop(stopCtx); err != nil {
			t.Logf("remote stop: %v", err)
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatal("remote A2A server did not become ready")
		}
		select {
		case err := <-serverErr:
			t.Fatalf("remote A2A server stopped: %v", err)
		default:
		}
		resp, err := http.Get(remoteURL + "/.well-known/agent-card.json")
		if err == nil {
			_ = resp.Body.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	clientA, err := a2aagent.New(a2aagent.WithAgentCardURL(remoteURL))
	if err != nil {
		t.Fatal(err)
	}
	clientB, err := a2aagent.New(a2aagent.WithAgentCardURL(remoteURL))
	if err != nil {
		t.Fatal(err)
	}

	sessionService := sessionmemory.NewSessionService()
	defer sessionService.Close()
	persistentKey := session.Key{AppName: "live-app", UserID: "live-user", SessionID: "live-session"}
	persistent, err := sessionService.CreateSession(ctx, persistentKey, session.StateMap{})
	if err != nil {
		t.Fatal(err)
	}
	parent := agent.NewInvocation(
		agent.WithInvocationSession(persistent),
		agent.WithInvocationSessionService(sessionService),
	)
	invoke := func(client *a2aagent.A2AAgent, id string) {
		t.Helper()
		invocation := parent.Clone(
			agent.WithInvocationID(id),
			agent.WithInvocationSession(&session.Session{AppName: persistentKey.AppName, ID: persistentKey.SessionID}),
			agent.WithInvocationMessage(model.NewUserMessage("live coordination")),
		)
		events, err := client.Run(ctx, invocation)
		if err != nil {
			t.Fatal(err)
		}
		for event := range events {
			if event != nil && event.Response != nil && event.Response.Error != nil {
				t.Fatalf("A2A response error: %v", event.Response.Error)
			}
		}
	}

	invoke(clientA, "live-first")
	invoke(clientB, "live-second")

	recorder.mu.Lock()
	userIDs := append([]string(nil), recorder.userID...)
	recorder.mu.Unlock()
	persisted, err := sessionService.GetSession(ctx, persistentKey)
	if err != nil {
		t.Fatal(err)
	}
	var persistedCookie []byte
	for key, value := range persisted.State {
		if strings.HasPrefix(key, "trpc.agent.a2a.anonymous_user_id_cookie.") && len(value) > 0 {
			persistedCookie = value
			break
		}
	}

	t.Logf("remote_url=%s", remoteURL)
	t.Logf("remote_request_count=%d", len(userIDs))
	t.Logf("remote_user_ids=%v", userIDs)
	t.Logf("persisted_cookie=%s", persistedCookie)
	if len(userIDs) != 2 || userIDs[0] == "" || userIDs[0] != userIDs[1] {
		t.Fatalf("expected two requests with one remote anonymous identity, got %v", userIDs)
	}
}
