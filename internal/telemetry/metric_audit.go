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
	// AuditMeter is the meter used for recording audit metrics.
	AuditMeter = MeterProvider.Meter(metrics.MeterNameAudit)

	// AuditMetricWriteFailedTotal records failed audit sink writes.
	AuditMetricWriteFailedTotal metric.Int64Counter
)

// AuditAttributes is the attributes for audit metrics.
type AuditAttributes struct {
	TenantID string
	AppName  string
	Decision string
	Error    error
}

func (a AuditAttributes) toAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(semconvtrace.KeyGenAIOperationName, OperationAuditWrite),
		attribute.String(semconvtrace.KeyGenAISystem, semconvtrace.SystemTRPCGoAgent),
	}
	if a.TenantID != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoTenantID, a.TenantID))
	}
	if a.AppName != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoAppName, a.AppName))
	}
	if a.Decision != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoAuditDecision, a.Decision))
	}
	if a.Error != nil {
		attrs = append(attrs, attribute.String(semconvtrace.KeyErrorType, ToErrorType(a.Error, semconvtrace.ValueDefaultErrorType)))
	}
	return attrs
}

// ReportAuditWriteFailedMetrics reports a failed audit sink write.
func ReportAuditWriteFailedMetrics(ctx context.Context, attrs AuditAttributes) {
	if AuditMetricWriteFailedTotal == nil {
		return
	}
	AuditMetricWriteFailedTotal.Add(ctx, 1, metric.WithAttributes(attrs.toAttributes()...))
}
