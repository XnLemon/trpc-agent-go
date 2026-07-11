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
	// GatewayMeter is the meter used for recording gateway metrics.
	GatewayMeter = MeterProvider.Meter(metrics.MeterNameGateway)

	// GatewayMetricBudgetDeniedTotal records gateway requests denied by budget checks.
	GatewayMetricBudgetDeniedTotal metric.Int64Counter
	// GatewayMetricRateLimitedTotal records inbound IM messages rejected by gateway rate limits.
	GatewayMetricRateLimitedTotal metric.Int64Counter
)

// GatewayBudgetDeniedAttributes is the attributes for gateway budget denial metrics.
type GatewayBudgetDeniedAttributes struct {
	TenantID string
	AppName  string
	Channel  string
	Reason   string
}

// GatewayRateLimitedAttributes is the attributes for gateway rate limit metrics.
type GatewayRateLimitedAttributes struct {
	TenantID string
	AppName  string
	Channel  string
}

func (a GatewayBudgetDeniedAttributes) toAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(semconvtrace.KeyGenAIOperationName, OperationGatewayBudget),
		attribute.String(semconvtrace.KeyGenAISystem, semconvtrace.SystemTRPCGoAgent),
	}
	if a.TenantID != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoTenantID, a.TenantID))
	}
	if a.AppName != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoAppName, a.AppName))
	}
	if a.Channel != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoChannel, a.Channel))
	}
	if a.Reason != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoBudgetDeniedReason, a.Reason))
	}
	return attrs
}

func (a GatewayRateLimitedAttributes) toAttributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(semconvtrace.KeyGenAIOperationName, OperationGatewayRateLimit),
		attribute.String(semconvtrace.KeyGenAISystem, semconvtrace.SystemTRPCGoAgent),
	}
	if a.TenantID != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoTenantID, a.TenantID))
	}
	if a.AppName != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoAppName, a.AppName))
	}
	if a.Channel != "" {
		attrs = append(attrs, attribute.String(semconvtrace.KeyTRPCAgentGoChannel, a.Channel))
	}
	return attrs
}

// ReportGatewayBudgetDeniedMetrics reports a gateway request denied by budget checks.
func ReportGatewayBudgetDeniedMetrics(ctx context.Context, attrs GatewayBudgetDeniedAttributes) {
	if GatewayMetricBudgetDeniedTotal == nil {
		return
	}
	GatewayMetricBudgetDeniedTotal.Add(ctx, 1, metric.WithAttributes(attrs.toAttributes()...))
}

// ReportGatewayRateLimitedMetrics reports an inbound IM message rejected by gateway rate limits.
func ReportGatewayRateLimitedMetrics(ctx context.Context, attrs GatewayRateLimitedAttributes) {
	if GatewayMetricRateLimitedTotal == nil {
		return
	}
	GatewayMetricRateLimitedTotal.Add(ctx, 1, metric.WithAttributes(attrs.toAttributes()...))
}
