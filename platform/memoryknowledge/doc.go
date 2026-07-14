//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

// Package memoryknowledge provides tenant-scoped memory and knowledge facades.
//
// The facade keeps the platform boundary in front of concrete backends:
// memory writes are accepted with eventual consistency because vector indexing
// or remote memory providers may lag, while knowledge reads always inject
// tenant_id and internal_user_id filters. SearchRequest MaxResults and MinScore
// remain caller-controlled latency, recall, and cost knobs.
package memoryknowledge
