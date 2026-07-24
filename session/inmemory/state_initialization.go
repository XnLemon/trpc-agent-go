//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2026 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package inmemory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/session"
)

type stateInitializationKey struct {
	sessionKey session.Key
	stateKey   string
}

type stateInitialization struct {
	done chan struct{}
}

// LoadOrInitializeSessionState implements session.StateInitializationService
// for sessions stored by s. Coordination is local to this service instance.
// It returns a caller-owned copy of an existing value with didInitialize false,
// or runs initialize without holding the session store lock and persists a
// caller-owned copy with didInitialize true. Present nil and empty values count
// as initialized. Callback errors and observed cancellation are returned
// without a write. Callback panics propagate after the per-state gate is
// released. A missing, expired, replaced, or concurrently modified session
// causes the operation to fail without overwriting the newer state.
func (s *SessionService) LoadOrInitializeSessionState(
	ctx context.Context,
	key session.Key,
	stateKey string,
	initialize func(context.Context) ([]byte, error),
) ([]byte, bool, error) {
	if ctx == nil {
		return nil, false, fmt.Errorf("memory session service initialize session state failed: context is nil")
	}
	if initialize == nil {
		return nil, false, fmt.Errorf("memory session service initialize session state failed: initializer is nil")
	}
	if err := key.CheckSessionKey(); err != nil {
		return nil, false, err
	}
	if strings.HasPrefix(stateKey, session.StateAppPrefix) {
		return nil, false, fmt.Errorf(
			"memory session service initialize session state failed: %s is not allowed, use UpdateAppState instead",
			stateKey,
		)
	}
	if strings.HasPrefix(stateKey, session.StateUserPrefix) {
		return nil, false, fmt.Errorf(
			"memory session service initialize session state failed: %s is not allowed, use UpdateUserState instead",
			stateKey,
		)
	}

	coordinationKey := stateInitializationKey{sessionKey: key, stateKey: stateKey}
	for {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		value, exists, err := s.loadSessionStateForInitialization(key, stateKey)
		if err != nil {
			return nil, false, err
		}
		if exists {
			return value, false, nil
		}

		initialization, owner := s.acquireStateInitialization(coordinationKey)
		if owner {
			return s.initializeSessionState(
				ctx,
				key,
				stateKey,
				coordinationKey,
				initialization,
				initialize,
			)
		}
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-initialization.done:
		}
	}
}

func (s *SessionService) initializeSessionState(
	ctx context.Context,
	key session.Key,
	stateKey string,
	coordinationKey stateInitializationKey,
	initialization *stateInitialization,
	initialize func(context.Context) ([]byte, error),
) ([]byte, bool, error) {
	defer s.finishStateInitialization(coordinationKey, initialization)

	ownerSession, value, exists, err := s.loadSessionStateOwner(key, stateKey)
	if err != nil {
		return nil, false, err
	}
	if exists {
		return value, false, nil
	}

	value, err = callStateInitializer(ctx, initialize)
	if err != nil {
		return nil, false, err
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if err := s.commitInitializedSessionState(ctx, key, stateKey, ownerSession, value); err != nil {
		return nil, false, err
	}
	return cloneStateValue(value), true, nil
}

func callStateInitializer(
	ctx context.Context,
	initialize func(context.Context) ([]byte, error),
) ([]byte, error) {
	initializeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	return initialize(initializeCtx)
}

func (s *SessionService) loadSessionStateForInitialization(
	key session.Key,
	stateKey string,
) ([]byte, bool, error) {
	_, value, exists, err := s.loadSessionStateOwner(key, stateKey)
	return value, exists, err
}

func (s *SessionService) loadSessionStateOwner(
	key session.Key,
	stateKey string,
) (*sessionWithTTL, []byte, bool, error) {
	app, ok := s.getAppSessions(key.AppName)
	if !ok {
		return nil, nil, false, fmt.Errorf(
			"memory session service initialize session state failed: session not found",
		)
	}

	app.mu.RLock()
	defer app.mu.RUnlock()
	userSessions, ok := app.sessions[key.UserID]
	if !ok {
		return nil, nil, false, fmt.Errorf(
			"memory session service initialize session state failed: session not found",
		)
	}
	storedSession, ok := userSessions[key.SessionID]
	if !ok {
		return nil, nil, false, fmt.Errorf(
			"memory session service initialize session state failed: session not found",
		)
	}
	if isExpired(storedSession.expiredAt) {
		return nil, nil, false, fmt.Errorf(
			"memory session service initialize session state failed: session expired",
		)
	}
	value, exists := storedSession.session.GetState(stateKey)
	return storedSession, value, exists, nil
}

func (s *SessionService) commitInitializedSessionState(
	ctx context.Context,
	key session.Key,
	stateKey string,
	ownerSession *sessionWithTTL,
	value []byte,
) error {
	app, ok := s.getAppSessions(key.AppName)
	if !ok {
		return fmt.Errorf("memory session service initialize session state failed: session was replaced")
	}

	app.mu.Lock()
	defer app.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	userSessions, ok := app.sessions[key.UserID]
	if !ok || userSessions[key.SessionID] != ownerSession {
		return fmt.Errorf("memory session service initialize session state failed: session was replaced")
	}
	if isExpired(ownerSession.expiredAt) {
		return fmt.Errorf("memory session service initialize session state failed: session expired")
	}
	if _, exists := ownerSession.session.GetState(stateKey); exists {
		return fmt.Errorf("memory session service initialize session state failed: state was initialized by another writer")
	}

	ownerSession.session.SetState(stateKey, value)
	ownerSession.session.UpdatedAt = time.Now()
	if s.opts.sessionTTL > 0 {
		ownerSession.expiredAt = calculateExpiredAt(s.opts.sessionTTL)
	}
	return nil
}

func (s *SessionService) acquireStateInitialization(
	key stateInitializationKey,
) (*stateInitialization, bool) {
	s.stateInitializationMu.Lock()
	defer s.stateInitializationMu.Unlock()
	if initialization, ok := s.stateInitializations[key]; ok {
		return initialization, false
	}
	initialization := &stateInitialization{done: make(chan struct{})}
	if s.stateInitializations == nil {
		s.stateInitializations = make(map[stateInitializationKey]*stateInitialization)
	}
	s.stateInitializations[key] = initialization
	return initialization, true
}

func (s *SessionService) finishStateInitialization(
	key stateInitializationKey,
	initialization *stateInitialization,
) {
	s.stateInitializationMu.Lock()
	defer s.stateInitializationMu.Unlock()
	if s.stateInitializations[key] != initialization {
		close(initialization.done)
		return
	}
	delete(s.stateInitializations, key)
	close(initialization.done)
}

func cloneStateValue(value []byte) []byte {
	if value == nil {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return cloned
}
