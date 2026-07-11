//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package graph

import (
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/platform"
)

func redactErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return redactErrorText(err.Error())
}

func redactErrorText(message string) string {
	redactor, err := platform.NewRedactor()
	if err != nil {
		return "redacted error detail unavailable"
	}
	return redactor.Redact(message)
}

func redactedResponseErrorFromError(err error, fallbackType string) *model.ResponseError {
	return redactResponseError(model.ResponseErrorFromError(err, fallbackType))
}

func redactedResponse(resp *model.Response) *model.Response {
	if resp == nil || resp.Error == nil {
		return resp
	}
	clone := resp.Clone()
	clone.Error = redactResponseError(resp.Error)
	return clone
}

func redactResponseError(respErr *model.ResponseError) *model.ResponseError {
	if respErr == nil {
		return nil
	}
	clone := *respErr
	if clone.Message != "" {
		clone.Message = redactErrorText(clone.Message)
	}
	if respErr.Code != nil {
		code := *respErr.Code
		code = redactErrorText(code)
		clone.Code = &code
	}
	if respErr.Param != nil {
		param := *respErr.Param
		param = redactErrorText(param)
		clone.Param = &param
	}
	return &clone
}
