//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package agent

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/skill"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// InvocationToolSurfaceProvider is an optional interface implemented by agents
// that can expose the effective, invocation-scoped tool surface together with
// the user-tool classification.
//
// The first return value is the full set of tools visible for the invocation
// (user tools plus framework-managed tools). The second return value maps the
// names that are classified as user tools (the tools registered via WithTools
// and WithToolSets) so callers can distinguish them from framework tools.
//
// It is intentionally a structural interface so that helpers such as the
// dynamic AgentTool can derive a child capability surface from a parent
// invocation without importing any concrete agent implementation.
type InvocationToolSurfaceProvider interface {
	InvocationToolSurface(
		ctx context.Context,
		inv *Invocation,
	) ([]tool.Tool, map[string]bool)
}

// InvocationToolActivationProvider is an optional interface implemented by
// agents that apply invocation-scoped activation after run-option tools have
// been appended to the base surface.
//
// The provider must return the activated tool surface together with updated
// user and external tool classifications. Callers provide private slice/map
// copies, so implementations may mutate the inputs without affecting the
// invocation's configured surface.
type InvocationToolActivationProvider interface {
	ApplyInvocationToolActivation(
		ctx context.Context,
		inv *Invocation,
		tools []tool.Tool,
		userToolNames map[string]bool,
		externalToolNames map[string]bool,
	) ([]tool.Tool, map[string]bool, map[string]bool)
}

// InvocationSkillRepositoryProvider is an optional interface implemented by
// agents that can expose the effective, invocation-scoped skill repository.
//
// The returned repository reflects any invocation-scoped surface overrides
// (for example a surface patch) layered on top of the agent's configured
// repository. It may return nil when the agent has no skills configured.
type InvocationSkillRepositoryProvider interface {
	InvocationSkillRepository(
		ctx context.Context,
		inv *Invocation,
	) skill.Repository
}

// InvocationCodeExecutorProvider is an optional interface implemented by
// agents that can expose the effective, invocation-scoped code executor.
//
// The returned executor reflects any per-run override (for example
// WithCodeExecutor) layered on top of the agent's configured executor. It may
// return nil when no executor is available for the invocation.
type InvocationCodeExecutorProvider interface {
	InvocationCodeExecutor(
		ctx context.Context,
		inv *Invocation,
	) codeexecutor.CodeExecutor
}
