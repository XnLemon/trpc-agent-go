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
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/metrics"
	semconvtrace "trpc.group/trpc-go/trpc-agent-go/telemetry/semconv/trace"
)

func TestReportGatewayBudgetDeniedMetricsNoopWhenCounterNil(t *testing.T) {
	originalCounter := GatewayMetricBudgetDeniedTotal
	t.Cleanup(func() {
		GatewayMetricBudgetDeniedTotal = originalCounter
	})

	GatewayMetricBudgetDeniedTotal = nil
	require.NotPanics(t, func() {
		ReportGatewayBudgetDeniedMetrics(context.Background(), GatewayBudgetDeniedAttributes{
			TenantID: "tenant-1",
			AppName:  "app-1",
			Channel:  "wecom",
			Reason:   "total_tokens_exceeded",
		})
	})
}

func TestReportGatewayBudgetDeniedMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	originalProvider := MeterProvider
	originalMeter := GatewayMeter
	originalCounter := GatewayMetricBudgetDeniedTotal
	t.Cleanup(func() {
		MeterProvider = originalProvider
		GatewayMeter = originalMeter
		GatewayMetricBudgetDeniedTotal = originalCounter
	})

	MeterProvider = provider
	GatewayMeter = provider.Meter(metrics.MeterNameGateway)
	var err error
	GatewayMetricBudgetDeniedTotal, err = GatewayMeter.Int64Counter(metrics.MetricGatewayBudgetDeniedTotal)
	require.NoError(t, err)

	ctx := context.Background()
	ReportGatewayBudgetDeniedMetrics(ctx, GatewayBudgetDeniedAttributes{
		TenantID: "tenant-1",
		AppName:  "app-1",
		Channel:  "wecom",
		Reason:   "total_tokens_exceeded",
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	points := gatewaySumPoints(t, rm, metrics.MetricGatewayBudgetDeniedTotal)
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[0].Value)
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyGenAIOperationName, OperationGatewayBudget)
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyGenAISystem, semconvtrace.SystemTRPCGoAgent)
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoTenantID, "tenant-1")
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoAppName, "app-1")
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoChannel, "wecom")
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoBudgetDeniedReason, "total_tokens_exceeded")
}

func TestReportGatewayRateLimitedMetricsNoopWhenCounterNil(t *testing.T) {
	originalCounter := GatewayMetricRateLimitedTotal
	t.Cleanup(func() {
		GatewayMetricRateLimitedTotal = originalCounter
	})

	GatewayMetricRateLimitedTotal = nil
	require.NotPanics(t, func() {
		ReportGatewayRateLimitedMetrics(context.Background(), GatewayRateLimitedAttributes{
			TenantID: "tenant-1",
			AppName:  "app-1",
			Channel:  "wecom",
		})
	})
}

func TestReportGatewayRateLimitedMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	originalProvider := MeterProvider
	originalMeter := GatewayMeter
	originalCounter := GatewayMetricRateLimitedTotal
	t.Cleanup(func() {
		MeterProvider = originalProvider
		GatewayMeter = originalMeter
		GatewayMetricRateLimitedTotal = originalCounter
	})

	MeterProvider = provider
	GatewayMeter = provider.Meter(metrics.MeterNameGateway)
	var err error
	GatewayMetricRateLimitedTotal, err = GatewayMeter.Int64Counter(metrics.MetricIMRateLimitedTotal)
	require.NoError(t, err)

	ctx := context.Background()
	ReportGatewayRateLimitedMetrics(ctx, GatewayRateLimitedAttributes{
		TenantID: "tenant-1",
		AppName:  "app-1",
		Channel:  "wecom",
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	points := gatewaySumPoints(t, rm, metrics.MetricIMRateLimitedTotal)
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[0].Value)
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyGenAIOperationName, OperationGatewayRateLimit)
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyGenAISystem, semconvtrace.SystemTRPCGoAgent)
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoTenantID, "tenant-1")
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoAppName, "app-1")
	requireGatewayAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoChannel, "wecom")
}

func gatewaySumPoints(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	metricName string,
) []metricdata.DataPoint[int64] {
	t.Helper()
	for _, scopeMetric := range rm.ScopeMetrics {
		for _, metric := range scopeMetric.Metrics {
			if metric.Name != metricName {
				continue
			}
			sum, ok := metric.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			return sum.DataPoints
		}
	}
	t.Fatalf("metric %s not found", metricName)
	return nil
}

func requireGatewayAttr(t *testing.T, set attribute.Set, key string, value string) {
	t.Helper()
	for _, kv := range set.ToSlice() {
		if string(kv.Key) == key {
			require.Equal(t, value, kv.Value.AsString())
			return
		}
	}
	t.Fatalf("attribute %s not found", key)
}
