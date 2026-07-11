//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package flow

import "trpc.group/trpc-go/trpc-agent-go/platform"

// RedactError returns a model-visible redacted error message.
func RedactError(err error) string {
	if err == nil {
		return ""
	}
	return RedactErrorText(err.Error())
}

// RedactErrorText redacts sensitive content from model-visible flow errors.
func RedactErrorText(message string) string {
	redactor, err := platform.NewRedactor()
	if err != nil {
		return "redacted error detail unavailable"
	}
	return redactor.Redact(message)
}
