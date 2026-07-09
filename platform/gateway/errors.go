//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package gateway

import "errors"

var (
	// ErrRuntimeNotFound indicates that no active runtime was registered for the inbound binding.
	ErrRuntimeNotFound = errors.New("gateway runtime not found")
	// ErrRuntimeInactive indicates that the tenant, app, or binding rejects runtime traffic.
	ErrRuntimeInactive = errors.New("gateway runtime inactive")
	// ErrRuntimeMismatch indicates that a runtime's tenant, app, binding, or inbound identifiers do not match.
	ErrRuntimeMismatch = errors.New("gateway runtime identifiers mismatch")
	// ErrBindingAccessDenied indicates that a binding policy rejects the inbound sender or conversation.
	ErrBindingAccessDenied = errors.New("gateway binding access denied")
	// ErrBindingMentionRequired indicates that a group/thread message did not mention the agent.
	ErrBindingMentionRequired = errors.New("gateway binding mention required")
	// ErrUnsupportedMessageType indicates that the gateway batch only supports text input.
	ErrUnsupportedMessageType = errors.New("gateway only supports text messages")
	// ErrEmptyText indicates that a text message does not contain usable text.
	ErrEmptyText = errors.New("gateway text content is required")
	// ErrRunnerResponseEmpty indicates that the runner completed without assistant text.
	ErrRunnerResponseEmpty = errors.New("gateway runner response is empty")
)
