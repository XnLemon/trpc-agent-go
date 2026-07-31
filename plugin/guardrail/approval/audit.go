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

// ContextWithAuditContext attaches trusted platform audit context.
func ContextWithAuditContext(ctx context.Context, auditCtx AuditContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, auditContextKey{}, auditCtx)
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
	if len(args) == 0 {
		return ""
	}
	sum := sha256.Sum256(args)
	return "args:sha256:" + hex.EncodeToString(sum[:]) + " args_bytes:" + strconv.Itoa(len(args))
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
