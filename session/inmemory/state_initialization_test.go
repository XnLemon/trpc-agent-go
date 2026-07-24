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
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

type stateInitializationResult struct {
	value         []byte
	didInitialize bool
	err           error
}

type doneObservedContext struct {
	context.Context
	doneObserved chan struct{}
	doneOnce     sync.Once
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.doneOnce.Do(func() { close(c.doneObserved) })
	return c.Context.Done()
}

func TestLoadOrInitializeSessionStateReturnsExistingValue(t *testing.T) {
	service, key := newStateInitializationTestService(t, session.StateMap{
		"principal": []byte("existing"),
	})

	var initializeCalls atomic.Int32
	value, didInitialize, err := service.LoadOrInitializeSessionState(
		context.Background(),
		key,
		"principal",
		func(context.Context) ([]byte, error) {
			initializeCalls.Add(1)
			return []byte("unexpected"), nil
		},
	)
	require.NoError(t, err)
	require.False(t, didInitialize)
	require.Equal(t, []byte("existing"), value)
	require.Zero(t, initializeCalls.Load())

	value[0] = 'X'
	persisted := getPersistedStateValue(t, service, key, "principal")
	require.Equal(t, []byte("existing"), persisted)
}

func TestLoadOrInitializeSessionStateTreatsPresentNilAsInitialized(t *testing.T) {
	service, key := newStateInitializationTestService(t, session.StateMap{
		"principal": nil,
	})

	var initializeCalls atomic.Int32
	value, didInitialize, err := service.LoadOrInitializeSessionState(
		context.Background(),
		key,
		"principal",
		func(context.Context) ([]byte, error) {
			initializeCalls.Add(1)
			return []byte("unexpected"), nil
		},
	)
	require.NoError(t, err)
	require.False(t, didInitialize)
	require.Nil(t, value)
	require.Zero(t, initializeCalls.Load())
}

func TestLoadOrInitializeSessionStatePersistsCallerOwnedValue(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	source := []byte("principal-1")

	value, didInitialize, err := service.LoadOrInitializeSessionState(
		context.Background(),
		key,
		"principal",
		func(context.Context) ([]byte, error) {
			return source, nil
		},
	)
	require.NoError(t, err)
	require.True(t, didInitialize)
	require.Equal(t, []byte("principal-1"), value)

	source[0] = 'X'
	value[1] = 'Y'
	persisted := getPersistedStateValue(t, service, key, "principal")
	require.Equal(t, []byte("principal-1"), persisted)
}

func TestLoadOrInitializeSessionStatePersistsNilValue(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)

	value, didInitialize, err := service.LoadOrInitializeSessionState(
		context.Background(),
		key,
		"principal",
		func(context.Context) ([]byte, error) { return nil, nil },
	)
	require.NoError(t, err)
	require.True(t, didInitialize)
	require.Nil(t, value)

	sess, err := service.GetSession(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, sess)
	persisted, exists := sess.GetState("principal")
	require.True(t, exists)
	require.Nil(t, persisted)
}

func TestLoadOrInitializeSessionStateCancelsCallbackContextAfterReturn(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	var callbackCtx context.Context

	_, _, err := service.LoadOrInitializeSessionState(
		context.Background(),
		key,
		"principal",
		func(ctx context.Context) ([]byte, error) {
			callbackCtx = ctx
			return []byte("principal-1"), nil
		},
	)
	require.NoError(t, err)
	require.NotNil(t, callbackCtx)
	select {
	case <-callbackCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("callback context remained active after callback returned")
	}
}

func TestLoadOrInitializeSessionStateCoordinatesConcurrentCallers(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	const callerCount = 16

	start := make(chan struct{})
	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	results := make(chan stateInitializationResult, callerCount)
	var initializeCalls atomic.Int32
	var ownerStartedOnce sync.Once

	for i := 0; i < callerCount; i++ {
		go func() {
			<-start
			value, didInitialize, err := service.LoadOrInitializeSessionState(
				context.Background(),
				key,
				"principal",
				func(context.Context) ([]byte, error) {
					initializeCalls.Add(1)
					ownerStartedOnce.Do(func() { close(ownerStarted) })
					<-releaseOwner
					return []byte("principal-1"), nil
				},
			)
			results <- stateInitializationResult{
				value:         value,
				didInitialize: didInitialize,
				err:           err,
			}
		}()
	}

	close(start)
	select {
	case <-ownerStarted:
	case <-time.After(time.Second):
		t.Fatal("initializer did not start")
	}
	close(releaseOwner)

	initializedCount := 0
	for i := 0; i < callerCount; i++ {
		result := <-results
		require.NoError(t, result.err)
		require.Equal(t, []byte("principal-1"), result.value)
		if result.didInitialize {
			initializedCount++
		}
	}
	require.Equal(t, int32(1), initializeCalls.Load())
	require.Equal(t, 1, initializedCount)
	require.Empty(t, service.stateInitializations)
}

func TestLoadOrInitializeSessionStateRecoversAfterOwnerFailure(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	ownerErr := errors.New("initialize failed")
	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	ownerResult := make(chan error, 1)

	go func() {
		_, _, err := service.LoadOrInitializeSessionState(
			context.Background(),
			key,
			"principal",
			func(context.Context) ([]byte, error) {
				close(ownerStarted)
				<-releaseOwner
				return nil, ownerErr
			},
		)
		ownerResult <- err
	}()
	<-ownerStarted

	waiterResult := make(chan stateInitializationResult, 1)
	go func() {
		value, didInitialize, err := service.LoadOrInitializeSessionState(
			context.Background(),
			key,
			"principal",
			func(context.Context) ([]byte, error) {
				return []byte("principal-2"), nil
			},
		)
		waiterResult <- stateInitializationResult{
			value:         value,
			didInitialize: didInitialize,
			err:           err,
		}
	}()

	close(releaseOwner)
	require.ErrorIs(t, <-ownerResult, ownerErr)
	result := <-waiterResult
	require.NoError(t, result.err)
	require.True(t, result.didInitialize)
	require.Equal(t, []byte("principal-2"), result.value)
}

func TestLoadOrInitializeSessionStateReleasesGateAfterInitializerPanic(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)

	require.Panics(t, func() {
		_, _, _ = service.LoadOrInitializeSessionState(
			context.Background(),
			key,
			"principal",
			func(context.Context) ([]byte, error) {
				panic("initializer panic")
			},
		)
	})

	value, didInitialize, err := service.LoadOrInitializeSessionState(
		context.Background(),
		key,
		"principal",
		func(context.Context) ([]byte, error) {
			return []byte("recovered"), nil
		},
	)
	require.NoError(t, err)
	require.True(t, didInitialize)
	require.Equal(t, []byte("recovered"), value)
}

func TestLoadOrInitializeSessionStateWaiterCancellation(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	ownerStarted := make(chan struct{})
	releaseOwner := make(chan struct{})
	ownerResult := make(chan error, 1)

	go func() {
		_, _, err := service.LoadOrInitializeSessionState(
			context.Background(),
			key,
			"principal",
			func(context.Context) ([]byte, error) {
				close(ownerStarted)
				<-releaseOwner
				return []byte("principal-1"), nil
			},
		)
		ownerResult <- err
	}()
	<-ownerStarted

	waiterBaseCtx, cancelWaiter := context.WithCancel(context.Background())
	waiterCtx := &doneObservedContext{
		Context:      waiterBaseCtx,
		doneObserved: make(chan struct{}),
	}
	var waiterInitializeCalls atomic.Int32
	waiterResult := make(chan error, 1)
	go func() {
		_, _, err := service.LoadOrInitializeSessionState(
			waiterCtx,
			key,
			"principal",
			func(context.Context) ([]byte, error) {
				waiterInitializeCalls.Add(1)
				return []byte("unexpected"), nil
			},
		)
		waiterResult <- err
	}()
	select {
	case <-waiterCtx.doneObserved:
	case <-time.After(time.Second):
		t.Fatal("waiter did not begin waiting for the owner")
	}
	cancelWaiter()
	require.ErrorIs(t, <-waiterResult, context.Canceled)
	require.Zero(t, waiterInitializeCalls.Load())

	close(releaseOwner)
	require.NoError(t, <-ownerResult)
}

func TestLoadOrInitializeSessionStateDoesNotCommitAfterCancellation(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	initializeStarted := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		_, _, err := service.LoadOrInitializeSessionState(
			ctx,
			key,
			"principal",
			func(ctx context.Context) ([]byte, error) {
				close(initializeStarted)
				<-ctx.Done()
				return []byte("must-not-persist"), nil
			},
		)
		result <- err
	}()
	<-initializeStarted
	cancel()

	require.ErrorIs(t, <-result, context.Canceled)
	sess, err := service.GetSession(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, sess)
	_, exists := sess.GetState("principal")
	require.False(t, exists)
}

func TestLoadOrInitializeSessionStateRejectsCompetingWrite(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	initializeStarted := make(chan struct{})
	releaseInitialize := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		_, _, err := service.LoadOrInitializeSessionState(
			context.Background(),
			key,
			"principal",
			func(context.Context) ([]byte, error) {
				close(initializeStarted)
				<-releaseInitialize
				return []byte("stale-owner"), nil
			},
		)
		result <- err
	}()
	<-initializeStarted
	require.NoError(t, service.UpdateSessionState(
		context.Background(),
		key,
		session.StateMap{"principal": []byte("external-writer")},
	))
	close(releaseInitialize)

	require.ErrorContains(t, <-result, "state was initialized by another writer")
	persisted := getPersistedStateValue(t, service, key, "principal")
	require.Equal(t, []byte("external-writer"), persisted)
}

func TestLoadOrInitializeSessionStateRejectsReplacedSession(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	initializeStarted := make(chan struct{})
	releaseInitialize := make(chan struct{})
	result := make(chan error, 1)

	go func() {
		_, _, err := service.LoadOrInitializeSessionState(
			context.Background(),
			key,
			"principal",
			func(context.Context) ([]byte, error) {
				close(initializeStarted)
				<-releaseInitialize
				return []byte("stale-owner"), nil
			},
		)
		result <- err
	}()
	<-initializeStarted
	require.NoError(t, service.DeleteSession(context.Background(), key))
	_, err := service.CreateSession(context.Background(), key, nil)
	require.NoError(t, err)
	close(releaseInitialize)

	require.ErrorContains(t, <-result, "session was replaced")
	sess, err := service.GetSession(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, sess)
	_, exists := sess.GetState("principal")
	require.False(t, exists)
}

func TestLoadOrInitializeSessionStateDoesNotBlockOtherStateKeys(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)

	go func() {
		_, _, err := service.LoadOrInitializeSessionState(
			context.Background(),
			key,
			"principal-a",
			func(context.Context) ([]byte, error) {
				close(firstStarted)
				<-releaseFirst
				return []byte("a"), nil
			},
		)
		firstResult <- err
	}()
	<-firstStarted

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, didInitialize, err := service.LoadOrInitializeSessionState(
		ctx,
		key,
		"principal-b",
		func(context.Context) ([]byte, error) {
			return []byte("b"), nil
		},
	)
	require.NoError(t, err)
	require.True(t, didInitialize)
	require.Equal(t, []byte("b"), value)

	close(releaseFirst)
	require.NoError(t, <-firstResult)
}

func TestLoadOrInitializeSessionStateDoesNotBlockOtherSessions(t *testing.T) {
	service, firstKey := newStateInitializationTestService(t, nil)
	secondKey := firstKey
	secondKey.SessionID = "state-initialization-session-2"
	_, err := service.CreateSession(context.Background(), secondKey, nil)
	require.NoError(t, err)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		_, _, err := service.LoadOrInitializeSessionState(
			context.Background(),
			firstKey,
			"principal",
			func(context.Context) ([]byte, error) {
				close(firstStarted)
				<-releaseFirst
				return []byte("first"), nil
			},
		)
		firstResult <- err
	}()
	<-firstStarted

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	value, didInitialize, err := service.LoadOrInitializeSessionState(
		ctx,
		secondKey,
		"principal",
		func(context.Context) ([]byte, error) { return []byte("second"), nil },
	)
	require.NoError(t, err)
	require.True(t, didInitialize)
	require.Equal(t, []byte("second"), value)

	close(releaseFirst)
	require.NoError(t, <-firstResult)
}

func TestLoadOrInitializeSessionStateValidatesInputs(t *testing.T) {
	service, key := newStateInitializationTestService(t, nil)
	initialize := func(context.Context) ([]byte, error) {
		return []byte("value"), nil
	}

	_, _, err := service.LoadOrInitializeSessionState(nil, key, "principal", initialize)
	require.ErrorContains(t, err, "context is nil")
	_, _, err = service.LoadOrInitializeSessionState(context.Background(), key, "principal", nil)
	require.ErrorContains(t, err, "initializer is nil")
	_, _, err = service.LoadOrInitializeSessionState(
		context.Background(), key, session.StateAppPrefix+"principal", initialize,
	)
	require.ErrorContains(t, err, "use UpdateAppState")
	_, _, err = service.LoadOrInitializeSessionState(
		context.Background(), key, session.StateUserPrefix+"principal", initialize,
	)
	require.ErrorContains(t, err, "use UpdateUserState")
	_, _, err = service.LoadOrInitializeSessionState(
		context.Background(),
		session.Key{AppName: "missing", UserID: "user", SessionID: "session"},
		"principal",
		initialize,
	)
	require.ErrorContains(t, err, "session not found")
}

func newStateInitializationTestService(
	t *testing.T,
	state session.StateMap,
) (*SessionService, session.Key) {
	t.Helper()
	service := NewSessionService()
	t.Cleanup(func() { require.NoError(t, service.Close()) })
	key := session.Key{
		AppName:   "state-initialization-app",
		UserID:    "state-initialization-user",
		SessionID: "state-initialization-session",
	}
	_, err := service.CreateSession(context.Background(), key, state)
	require.NoError(t, err)
	return service, key
}

func getPersistedStateValue(
	t *testing.T,
	service *SessionService,
	key session.Key,
	stateKey string,
) []byte {
	t.Helper()
	sess, err := service.GetSession(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, sess)
	value, exists := sess.GetState(stateKey)
	require.True(t, exists)
	return value
}
