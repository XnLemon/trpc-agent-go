//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package approval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/platform"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// AuditContext carries trusted platform context for tool approval audit.
type AuditContext struct {
	TenantID  string
	AppID     string
	RequestID string
	TraceID   string
}

type auditContextKey struct{}
type approvedToolCallContextKey struct{}

// ContextWithAuditContext attaches trusted platform audit context.
func ContextWithAuditContext(ctx context.Context, auditCtx AuditContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, auditContextKey{}, auditCtx)
}

// ApprovedToolCall binds approval to the exact tool call payload reviewed by
// the approval plugin.
type ApprovedToolCall struct {
	ToolCallID     string
	ToolName       string
	ArgumentsHash  string
	ArgumentsBytes int
	Metadata       tool.ToolMetadata
}

// contextWithApprovedToolCall marks a reviewed tool call as approved so later
// mandatory permission checks do not ask for the same approval again.
func contextWithApprovedToolCall(ctx context.Context, args *tool.BeforeToolArgs) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	fingerprint := approvedToolCallFingerprint(args)
	if fingerprint.ToolCallID == "" {
		return ctx
	}
	return context.WithValue(
		ctx,
		approvedToolCallContextKey{},
		fingerprint,
	)
}

// ApprovedToolCallFromContext returns the approved tool call fingerprint, if any.
func ApprovedToolCallFromContext(ctx context.Context) (ApprovedToolCall, bool) {
	if ctx == nil {
		return ApprovedToolCall{}, false
	}
	fingerprint, ok := ctx.Value(approvedToolCallContextKey{}).(ApprovedToolCall)
	fingerprint.ToolCallID = strings.TrimSpace(fingerprint.ToolCallID)
	fingerprint.ToolName = strings.TrimSpace(fingerprint.ToolName)
	return fingerprint, ok && fingerprint.ToolCallID != ""
}

func approvedToolCallFingerprint(args *tool.BeforeToolArgs) ApprovedToolCall {
	if args == nil {
		return ApprovedToolCall{}
	}
	hash, bytes := argumentDigest(args.Arguments)
	return ApprovedToolCall{
		ToolCallID:     strings.TrimSpace(args.ToolCallID),
		ToolName:       strings.TrimSpace(args.ToolName),
		ArgumentsHash:  hash,
		ArgumentsBytes: bytes,
		Metadata:       args.Metadata,
	}
}

func (p *Plugin) writeApprovalAudit(
	ctx context.Context,
	args *tool.BeforeToolArgs,
	decision platform.ToolApprovalDecision,
	reason string,
	approverUserID string,
) error {
	if p == nil || p.auditSink == nil || args == nil {
		return nil
	}
	auditCtx := approvalAuditContextFrom(ctx)
	record, err := platform.NewToolApprovalAuditRecord(platform.ToolApprovalAuditInput{
		TenantID:           auditCtx.TenantID,
		AppID:              auditCtx.AppID,
		ToolName:           args.ToolName,
		ToolCallID:         args.ToolCallID,
		Decision:           decision,
		DecisionReason:     reason,
		ApproverUserID:     approverUserID,
		RequestID:          auditCtx.RequestID,
		TraceID:            auditCtx.TraceID,
		ArgumentSummaryRef: argumentSummaryRef(args.Arguments),
		CreatedAt:          p.auditNow(),
	})
	if err != nil {
		return err
	}
	return p.auditSink.WriteAudit(ctx, record)
}

func (p *Plugin) auditApproverUserID() string {
	if p == nil {
		return ""
	}
	return p.approverUserID
}

func (p *Plugin) auditNow() time.Time {
	if p == nil || p.now == nil {
		return time.Now()
	}
	return p.now()
}

func approvalAuditContextFrom(ctx context.Context) AuditContext {
	var auditCtx AuditContext
	if ctx != nil {
		if value, ok := ctx.Value(auditContextKey{}).(AuditContext); ok {
			auditCtx = AuditContext{
				TenantID:  strings.TrimSpace(value.TenantID),
				AppID:     strings.TrimSpace(value.AppID),
				RequestID: strings.TrimSpace(value.RequestID),
				TraceID:   strings.TrimSpace(value.TraceID),
			}
		}
	}
	if invocation, ok := agent.InvocationFromContext(ctx); ok && invocation != nil {
		if auditCtx.AppID == "" && invocation.Session != nil {
			auditCtx.AppID = strings.TrimSpace(invocation.Session.AppName)
		}
		if auditCtx.RequestID == "" {
			auditCtx.RequestID = strings.TrimSpace(invocation.RunOptions.RequestID)
		}
	}
	if auditCtx.TraceID == "" {
		auditCtx.TraceID = auditCtx.RequestID
	}
	return auditCtx
}

func argumentSummaryRef(args []byte) string {
	hash, bytes := argumentDigest(args)
	if hash == "" {
		return ""
	}
	return "args:sha256:" + hash + " args_bytes:" + strconv.Itoa(bytes)
}

func argumentDigest(args []byte) (string, int) {
	if len(args) == 0 {
		return "", 0
	}
	sum := sha256.Sum256(args)
	return hex.EncodeToString(sum[:]), len(args)
}

func approvalAuditDecisionReason(decision platform.ToolApprovalDecision) string {
	switch decision {
	case platform.ToolApprovalDecisionRequested:
		return "tool approval requested"
	case platform.ToolApprovalDecisionApproved:
		return "tool approval approved"
	case platform.ToolApprovalDecisionRejected:
		return "tool approval rejected"
	default:
		return ""
	}
}
