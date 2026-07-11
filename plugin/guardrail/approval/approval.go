//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

// Package approval provides a runner-scoped tool approval plugin.
package approval

import (
	"context"
	"fmt"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/plugin"
	"trpc.group/trpc-go/trpc-agent-go/plugin/guardrail/approval/review"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// Plugin is the approval plugin implementation.
type Plugin struct {
	name              string
	reviewer          review.Reviewer
	defaultToolPolicy ToolPolicy
	toolPolicies      map[string]ToolPolicy
	metadataPolicy    ToolPolicy
	tokenCounter      model.TokenCounter
	auditSink         platform.AuditSink
	approverUserID    string
	now               func() time.Time
}

// New creates a new approval plugin.
func New(options ...Option) (*Plugin, error) {
	opts := newOptions(options...)
	if err := validateToolPolicy(opts.defaultToolPolicy); err != nil {
		return nil, fmt.Errorf("newing approval plugin: default tool policy: %w", err)
	}
	if err := validateToolPolicy(opts.metadataPolicy); err != nil {
		return nil, fmt.Errorf("newing approval plugin: metadata policy: %w", err)
	}
	for toolName, policy := range opts.toolPolicies {
		if toolName == "" {
			return nil, fmt.Errorf("newing approval plugin: tool policy name is empty")
		}
		if err := validateToolPolicy(policy); err != nil {
			return nil, fmt.Errorf("newing approval plugin: tool %q policy: %w", toolName, err)
		}
	}
	if requiresReviewer(opts) && opts.reviewer == nil {
		return nil, fmt.Errorf("newing approval plugin: reviewer is nil")
	}
	if opts.auditSink != nil && requiresReviewer(opts) && strings.TrimSpace(opts.approverUserID) == "" {
		return nil, fmt.Errorf("newing approval plugin: approver user id is required when approval audit is enabled")
	}
	return &Plugin{
		name:              opts.name,
		reviewer:          opts.reviewer,
		defaultToolPolicy: opts.defaultToolPolicy,
		toolPolicies:      opts.toolPolicies,
		metadataPolicy:    opts.metadataPolicy,
		tokenCounter:      model.NewSimpleTokenCounter(),
		auditSink:         opts.auditSink,
		approverUserID:    strings.TrimSpace(opts.approverUserID),
		now:               opts.now,
	}, nil
}

// Name implements plugin.Plugin.
func (p *Plugin) Name() string {
	return p.name
}

// Register implements plugin.Plugin.
func (p *Plugin) Register(r *plugin.Registry) {
	if p == nil || r == nil {
		return
	}
	r.BeforeTool(p.beforeTool())
}

func (p *Plugin) beforeTool() tool.BeforeToolCallbackStructured {
	return func(ctx context.Context, args *tool.BeforeToolArgs) (*tool.BeforeToolResult, error) {
		if args == nil {
			return nil, nil
		}
		policy := p.resolvePolicy(args)
		switch policy {
		case ToolPolicyDenied:
			return &tool.BeforeToolResult{
				CustomResult: fmt.Sprintf("tool %q is denied by approval policy", args.ToolName),
			}, nil
		case ToolPolicySkipApproval:
			return nil, nil
		case ToolPolicyRequireApproval:
			req, err := p.buildRequest(ctx, args)
			if err != nil {
				log.ErrorfContext(
					ctx,
					"Automatic approval review denied: approval review failed for tool %q: %v",
					args.ToolName,
					err,
				)
				return &tool.BeforeToolResult{
					CustomResult: fmt.Sprintf("approval review failed for tool %q: %v", args.ToolName, err),
				}, nil
			}
			if err := p.writeApprovalAudit(
				ctx,
				args,
				platform.ToolApprovalDecisionRequested,
				approvalAuditDecisionReason(platform.ToolApprovalDecisionRequested),
				"",
			); err != nil {
				log.ErrorfContext(
					ctx,
					"Automatic approval review denied: approval audit failed for tool %q: %v",
					args.ToolName,
					err,
				)
				return &tool.BeforeToolResult{
					CustomResult: fmt.Sprintf("approval audit failed for tool %q: %v", args.ToolName, err),
				}, nil
			}
			reportApprovalRequiredMetric(ctx, args)
			decision, err := p.reviewer.Review(ctx, req)
			if err != nil {
				log.ErrorfContext(
					ctx,
					"Automatic approval review denied: approval review failed for tool %q: %v",
					args.ToolName,
					err,
				)
				return &tool.BeforeToolResult{
					CustomResult: fmt.Sprintf("approval review failed for tool %q: %v", args.ToolName, err),
				}, nil
			}
			if decision == nil {
				err = fmt.Errorf("approval reviewer returned nil decision")
				log.ErrorfContext(
					ctx,
					"Automatic approval review denied: approval review failed for tool %q: %v",
					args.ToolName,
					err,
				)
				return &tool.BeforeToolResult{
					CustomResult: fmt.Sprintf("approval review failed for tool %q: %v", args.ToolName, err),
				}, nil
			}
			riskLevel := sanitizeReviewerText(decision.RiskLevel)
			reason := sanitizeReviewerText(decision.Reason)
			if decision.Approved {
				if err := p.writeApprovalAudit(
					ctx,
					args,
					platform.ToolApprovalDecisionApproved,
					approvalAuditDecisionReason(platform.ToolApprovalDecisionApproved),
					p.auditApproverUserID(),
				); err != nil {
					log.ErrorfContext(
						ctx,
						"Automatic approval review denied: approval audit failed for tool %q: %v",
						args.ToolName,
						err,
					)
					return &tool.BeforeToolResult{
						CustomResult: fmt.Sprintf("approval audit failed for tool %q: %v", args.ToolName, err),
					}, nil
				}
				log.InfofContext(
					ctx,
					"Automatic approval review approved (risk: %s): %s",
					riskLevel,
					reason,
				)
				if strings.TrimSpace(args.ToolCallID) == "" {
					return nil, nil
				}
				return &tool.BeforeToolResult{
					Context: contextWithApprovedToolCall(ctx, args),
				}, nil
			}
			denyMessage := fmt.Sprintf(
				"Automatic approval review denied (risk: %s): %s",
				riskLevel,
				reason,
			)
			if err := p.writeApprovalAudit(
				ctx,
				args,
				platform.ToolApprovalDecisionRejected,
				approvalAuditDecisionReason(platform.ToolApprovalDecisionRejected),
				p.auditApproverUserID(),
			); err != nil {
				log.ErrorfContext(
					ctx,
					"Automatic approval review denied: approval audit failed for tool %q: %v",
					args.ToolName,
					err,
				)
				return &tool.BeforeToolResult{
					CustomResult: fmt.Sprintf("approval audit failed for tool %q: %v", args.ToolName, err),
				}, nil
			}
			log.WarnContext(ctx, denyMessage)
			return &tool.BeforeToolResult{
				CustomResult: tool.ApprovalDeniedResultFor(args.ToolName, denyMessage),
			}, nil
		default:
			return &tool.BeforeToolResult{
				CustomResult: fmt.Sprintf("approval review failed for tool %q: %v", args.ToolName, fmt.Errorf("unsupported tool policy %q", policy)),
			}, nil
		}
	}
}

func sanitizeReviewerText(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	redactor, err := platform.NewRedactor()
	if err != nil {
		return value
	}
	return redactor.Redact(value)
}

func (p *Plugin) resolvePolicy(args *tool.BeforeToolArgs) ToolPolicy {
	if args == nil {
		return p.defaultToolPolicy
	}
	policy, explicit := p.toolPolicies[args.ToolName]
	if explicit && policy != ToolPolicySkipApproval {
		return policy
	}
	if p.defaultToolPolicy == ToolPolicyDenied && !explicit {
		return p.defaultToolPolicy
	}
	if metadataHighRisk(args.Metadata) {
		return p.metadataPolicy
	}
	if explicit {
		return policy
	}
	return p.defaultToolPolicy
}

func requiresReviewer(opts *options) bool {
	if opts.defaultToolPolicy == ToolPolicyRequireApproval {
		return true
	}
	if opts.metadataPolicy == ToolPolicyRequireApproval {
		return true
	}
	for _, policy := range opts.toolPolicies {
		if policy == ToolPolicyRequireApproval {
			return true
		}
	}
	return false
}

func metadataHighRisk(metadata tool.ToolMetadata) bool {
	if metadata == (tool.ToolMetadata{}) {
		return false
	}
	return metadata.Destructive || !metadata.ReadOnly || metadata.OpenWorld
}
