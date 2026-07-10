//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package worker

import "errors"

var (
	// ErrStorageRouterRequired indicates that runtime storage cannot be resolved.
	ErrStorageRouterRequired = errors.New("worker storage router is required")
	// ErrAgentFactoryRequired indicates that no tenant agent factory was configured.
	ErrAgentFactoryRequired = errors.New("worker agent factory is required")
	// ErrAppNameRequired indicates that the runtime app has no storage app name.
	ErrAppNameRequired = errors.New("worker app_name is required")
	// ErrAgentNameRequired indicates that the runtime app has no agent identity.
	ErrAgentNameRequired = errors.New("worker agent_name is required")
	// ErrStorageProfileIDRequired indicates that the app has no storage profile.
	ErrStorageProfileIDRequired = errors.New("worker storage_profile_id is required")
	// ErrRuntimeIdentityMismatch indicates that tenant, app, and binding disagree.
	ErrRuntimeIdentityMismatch = errors.New("worker runtime identity mismatch")
	// ErrAgentRequired indicates that the factory returned no agent.
	ErrAgentRequired = errors.New("worker agent is required")
	// ErrAgentNameMismatch indicates that the built agent does not match app config.
	ErrAgentNameMismatch = errors.New("worker agent name does not match app config")
)
