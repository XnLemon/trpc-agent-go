//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/metrics"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
)

var (
	// ToolApprovalMeter is the meter used for recording tool approval metrics.
	ToolApprovalMeter = MeterProvider.Meter(metrics.MeterNameToolApproval)

	// ToolApprovalMetricRequiredTotal records tool calls that require explicit approval.
	ToolApprovalMetricRequiredTotal metric.Int64Counter
)

// ToolApprovalAttributes is the attributes for tool approval metrics.
type ToolApprovalAttributes struct {
	TenantID string
	AppName  string
	ToolName string
}

func (a ToolApprovalAttributes) toAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(semconvtrace.KeyGenAIOperationName, OperationToolApproval),
		attribute.String(semconvtrace.KeyGenAISystem, semconvtrace.SystemTRPCGoAgent),
		attribute.String(semconvtrace.KeyGenAIToolName, a.ToolName),
	}
	if a.TenantID != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoTenantID, a.TenantID))
	}
	if a.AppName != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoAppName, a.AppName))
	}
	return attrs
}

// ReportToolApprovalRequiredMetrics reports that a tool call required explicit approval.
func ReportToolApprovalRequiredMetrics(ctx context.Context, attrs ToolApprovalAttributes) {
	if ToolApprovalMetricRequiredTotal == nil {
		return
	}
	ToolApprovalMetricRequiredTotal.Add(ctx, 1, metric.WithAttributes(attrs.toAttributes()...))
}
