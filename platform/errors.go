//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import "errors"

var (
	// ErrTenantIDRequired indicates a missing tenant identifier.
	ErrTenantIDRequired = errors.New("tenant_id is required")
	// ErrAppIDRequired indicates a missing app identifier.
	ErrAppIDRequired = errors.New("app_id is required")
	// ErrBindingIDRequired indicates a missing channel binding identifier.
	ErrBindingIDRequired = errors.New("binding_id is required")
	// ErrChannelRequired indicates a missing channel identifier.
	ErrChannelRequired = errors.New("channel is required")
	// ErrAccountIDRequired indicates a missing channel account identifier.
	ErrAccountIDRequired = errors.New("account_id is required")
	// ErrPlatformMessageIDRequired indicates a missing platform message identifier.
	ErrPlatformMessageIDRequired = errors.New("platform_message_id is required")
	// ErrIdempotencyRecordNotFound indicates an unknown idempotency key.
	ErrIdempotencyRecordNotFound = errors.New("idempotency record not found")
	// ErrExternalUserIDRequired indicates a missing external user identifier.
	ErrExternalUserIDRequired = errors.New("external_user_id is required")
	// ErrExternalGroupIDRequired indicates a missing group identifier.
	ErrExternalGroupIDRequired = errors.New("external_group_id is required")
	// ErrConversationTypeRequired indicates a missing conversation type.
	ErrConversationTypeRequired = errors.New("conversation_type is required")
	// ErrInvalidConversationType indicates an unsupported conversation type.
	ErrInvalidConversationType = errors.New("invalid conversation_type")
	// ErrSecretReferenceRequired indicates a configuration contains inline secret material.
	ErrSecretReferenceRequired = errors.New("secret reference is required")
	// ErrInlineSecretRejected indicates a configuration appears to contain inline secret material.
	ErrInlineSecretRejected = errors.New("inline secret values are not allowed")
	// ErrWebhookPathRequired indicates a missing webhook path.
	ErrWebhookPathRequired = errors.New("webhook_path is required")
)
