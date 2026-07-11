//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package approval

import (
	"time"

	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/plugin/guardrail/approval/review"
)

const defaultPluginName = "approval"

// Option configures the approval plugin.
type Option func(*options)

type options struct {
	name              string
	reviewer          review.Reviewer
	defaultToolPolicy ToolPolicy
	toolPolicies      map[string]ToolPolicy
	metadataPolicy    ToolPolicy
	auditSink         platform.AuditSink
	approverUserID    string
	now               func() time.Time
}

func newOptions(opts ...Option) *options {
	options := &options{
		name:              defaultPluginName,
		defaultToolPolicy: ToolPolicyRequireApproval,
		toolPolicies:      make(map[string]ToolPolicy),
		metadataPolicy:    ToolPolicySkipApproval,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(options)
		}
	}
	return options
}

// WithName sets the plugin name.
func WithName(name string) Option {
	return func(opts *options) {
		opts.name = name
	}
}

// WithReviewer sets the reviewer used for approval-required tool calls.
func WithReviewer(reviewer review.Reviewer) Option {
	return func(opts *options) {
		opts.reviewer = reviewer
	}
}

// WithDefaultToolPolicy sets the default policy used when no explicit tool policy exists.
func WithDefaultToolPolicy(policy ToolPolicy) Option {
	return func(opts *options) {
		opts.defaultToolPolicy = policy
	}
}

// WithToolPolicy sets the policy for a single tool name.
func WithToolPolicy(name string, policy ToolPolicy) Option {
	return func(opts *options) {
		if opts.toolPolicies == nil {
			opts.toolPolicies = make(map[string]ToolPolicy)
		}
		opts.toolPolicies[name] = policy
	}
}

// WithMetadataRiskPolicy sets the policy for calls whose tool metadata is
// high-risk when no explicit tool policy exists.
func WithMetadataRiskPolicy(policy ToolPolicy) Option {
	return func(opts *options) {
		opts.metadataPolicy = policy
	}
}

// WithAuditSink records approval request and decision boundaries to audit.
func WithAuditSink(sink platform.AuditSink) Option {
	return func(opts *options) {
		opts.auditSink = sink
	}
}

// WithApproverUserID sets the stable identity used for automatic reviewer decisions.
func WithApproverUserID(userID string) Option {
	return func(opts *options) {
		opts.approverUserID = userID
	}
}

func withNow(now func() time.Time) Option {
	return func(opts *options) {
		if now != nil {
			opts.now = now
		}
	}
}
