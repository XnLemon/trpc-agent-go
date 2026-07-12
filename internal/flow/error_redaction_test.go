//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package flow

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRedactErrorMasksSensitiveDetails(t *testing.T) {
	err := errors.New("failed Authorization: Bearer raw-token api_key=sk-testsecret token=raw-token secret: raw-secret password=raw-password Cookie: session=raw-cookie")

	got := RedactError(err)

	require.Contains(t, got, "failed")
	require.Contains(t, got, "****")
	for _, leaked := range []string{
		"raw-token",
		"sk-testsecret",
		"raw-secret",
		"raw-password",
		"raw-cookie",
	} {
		require.NotContains(t, got, leaked)
	}
}
