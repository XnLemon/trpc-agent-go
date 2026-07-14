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
	// ErrStorageAdapterRequired indicates that no tenant storage adapter was resolved.
	ErrStorageAdapterRequired = errors.New("worker storage adapter is required")
	// ErrSessionServiceRequired indicates that runtime session storage is missing.
	ErrSessionServiceRequired = errors.New("worker session service is required")
	// ErrMemoryServiceRequired indicates that runtime memory storage is missing.
	ErrMemoryServiceRequired = errors.New("worker memory service is required")
	// ErrArtifactServiceRequired indicates that runtime artifact storage is missing.
	ErrArtifactServiceRequired = errors.New("worker artifact service is required")
	// ErrKnowledgeServiceRequired indicates that runtime knowledge storage is missing.
	ErrKnowledgeServiceRequired = errors.New("worker knowledge service is required")
	// ErrAuditSinkRequired indicates that runtime audit storage is missing.
	ErrAuditSinkRequired = errors.New("worker audit sink is required")
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
	// ErrToolPolicyProviderRequired indicates that an app policy cannot be resolved.
	ErrToolPolicyProviderRequired = errors.New("worker tool policy provider is required")
	// ErrToolPolicyIdentityMismatch indicates that a resolved policy belongs elsewhere.
	ErrToolPolicyIdentityMismatch = errors.New("worker tool policy identity mismatch")
)
