//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2026 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package inmemory_test

import (
	"context"
	"fmt"

	"trpc.group/trpc-go/trpc-agent-go/session"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
)

func ExampleSessionService_LoadOrInitializeSessionState() {
	ctx := context.Background()
	var service session.Service = inmemory.NewSessionService()
	defer service.Close()
	key := session.Key{AppName: "app", UserID: "user", SessionID: "session"}
	if _, err := service.CreateSession(ctx, key, nil); err != nil {
		panic(err)
	}

	initializer, ok := service.(session.StateInitializationService)
	if !ok {
		panic("session service does not support coordinated state initialization")
	}
	value, didInitialize, err := initializer.LoadOrInitializeSessionState(
		ctx,
		key,
		"remote:principal",
		func(context.Context) ([]byte, error) {
			return []byte("principal-1"), nil
		},
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(value), didInitialize)

	value, didInitialize, err = initializer.LoadOrInitializeSessionState(
		ctx,
		key,
		"remote:principal",
		func(context.Context) ([]byte, error) {
			return []byte("principal-2"), nil
		},
	)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(value), didInitialize)

	// Output:
	// principal-1 true
	// principal-1 false
}
