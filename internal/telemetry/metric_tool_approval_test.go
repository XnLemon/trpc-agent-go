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

func TestReportToolApprovalRequiredMetricsNoopWhenCounterNil(t *testing.T) {
	originalCounter := ToolApprovalMetricRequiredTotal
	t.Cleanup(func() {
		ToolApprovalMetricRequiredTotal = originalCounter
	})

	ToolApprovalMetricRequiredTotal = nil
	require.NotPanics(t, func() {
		ReportToolApprovalRequiredMetrics(context.Background(), ToolApprovalAttributes{
			TenantID: "tenant-1",
			AppName:  "app-1",
			ToolName: "shell",
		})
	})
}

func TestReportToolApprovalRequiredMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))

	originalProvider := MeterProvider
	originalMeter := ToolApprovalMeter
	originalCounter := ToolApprovalMetricRequiredTotal
	t.Cleanup(func() {
		MeterProvider = originalProvider
		ToolApprovalMeter = originalMeter
		ToolApprovalMetricRequiredTotal = originalCounter
	})

	MeterProvider = provider
	ToolApprovalMeter = provider.Meter(metrics.MeterNameToolApproval)
	var err error
	ToolApprovalMetricRequiredTotal, err = ToolApprovalMeter.Int64Counter(metrics.MetricToolApprovalRequiredTotal)
	require.NoError(t, err)

	ctx := context.Background()
	ReportToolApprovalRequiredMetrics(ctx, ToolApprovalAttributes{
		TenantID: "tenant-1",
		AppName:  "app-1",
		ToolName: "shell",
	})

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	points := toolApprovalSumPoints(t, rm, metrics.MetricToolApprovalRequiredTotal)
	require.Len(t, points, 1)
	require.Equal(t, int64(1), points[0].Value)
	requireToolApprovalAttr(t, points[0].Attributes, semconvtrace.KeyGenAIOperationName, OperationToolApproval)
	requireToolApprovalAttr(t, points[0].Attributes, semconvtrace.KeyGenAISystem, semconvtrace.SystemTRPCGoAgent)
	requireToolApprovalAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoTenantID, "tenant-1")
	requireToolApprovalAttr(t, points[0].Attributes, semconvtrace.KeyTRPCAgentGoAppName, "app-1")
	requireToolApprovalAttr(t, points[0].Attributes, semconvtrace.KeyGenAIToolName, "shell")
}

func toolApprovalSumPoints(
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

func requireToolApprovalAttr(t *testing.T, set attribute.Set, key string, value string) {
	t.Helper()
	for _, kv := range set.ToSlice() {
		if string(kv.Key) == key {
			require.Equal(t, value, kv.Value.AsString())
			return
		}
	}
	t.Fatalf("attribute %s not found", key)
}
