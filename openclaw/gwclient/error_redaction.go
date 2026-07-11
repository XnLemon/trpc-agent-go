//
// Tencent is pleased to support the open source community by making
// trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package gwclient

import "trpc.group/trpc-go/trpc-agent-go/platform"

func redactErrorText(message string) string {
	redactor, err := platform.NewRedactor()
	if err != nil {
		return "redacted error detail unavailable"
	}
	return redactor.Redact(message)
}
