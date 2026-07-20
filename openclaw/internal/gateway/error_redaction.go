//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package gateway

import (
	"trpc.group/trpc-go/trpc-agent-go/openclaw/gwproto"
	"trpc.group/trpc-go/trpc-agent-go/platform"
)

func redactErrorText(message string) string {
	redactor, err := platform.NewRedactor()
	if err != nil {
		return "redacted error detail unavailable"
	}
	return redactor.Redact(message)
}

func redactGatewayAPIError(err gwproto.APIError) gwproto.APIError {
	err.Type = redactErrorText(err.Type)
	err.Message = redactErrorText(err.Message)
	return err
}
