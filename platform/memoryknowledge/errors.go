//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package memoryknowledge

import "errors"

var (
	// ErrMemoryBackendRequired indicates that the facade has no memory backend.
	ErrMemoryBackendRequired = errors.New("memory backend is required")
	// ErrKnowledgeBackendRequired indicates that the facade has no knowledge backend.
	ErrKnowledgeBackendRequired = errors.New("knowledge backend is required")
	// ErrInternalUserIDRequired indicates that retrieval lacks the internal user boundary.
	ErrInternalUserIDRequired = errors.New("internal_user_id is required")
	// ErrNamespaceRequired indicates that the storage namespace is missing.
	ErrNamespaceRequired = errors.New("namespace is required")
	// ErrFilterOutsideScope indicates that caller-supplied retrieval filters escape the scope.
	ErrFilterOutsideScope = errors.New("memoryknowledge filter outside scope")
)
