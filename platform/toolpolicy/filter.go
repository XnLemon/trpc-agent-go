//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package toolpolicy

import (
	"context"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// ToolFilter returns the run-scoped visibility filter for this policy.
//
// A non-empty whitelist is an allow boundary. Tool and platform denylists
// always override the whitelist. Nil is returned when the policy does not
// constrain tool names.
func (p *Policy) ToolFilter() tool.FilterFunc {
	if p == nil {
		return nil
	}
	whitelist := nameSet(normalizedList(p.policy.ToolWhitelist))
	denylist := nameSet(policyDenylist(p.policy))
	if len(whitelist) == 0 && len(denylist) == 0 {
		return nil
	}
	return func(_ context.Context, candidate tool.Tool) bool {
		if candidate == nil || candidate.Declaration() == nil {
			return false
		}
		name := strings.TrimSpace(candidate.Declaration().Name)
		if name == "" {
			return false
		}
		if _, denied := denylist[name]; denied {
			return false
		}
		if len(whitelist) == 0 {
			return true
		}
		_, allowed := whitelist[name]
		return allowed
	}
}

func nameSet(names []string) map[string]struct{} {
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		set[name] = struct{}{}
	}
	return set
}
