//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

var rawSecretPrefixes = []string{
	"sk-",
	"xoxb-",
	"xoxp-",
	"ya29.",
	"ghp_",
	"github_pat_",
	"glpat-",
}

// Validate checks that the tenant can be used as an isolation boundary.
func (t Tenant) Validate() error {
	if strings.TrimSpace(t.TenantID) == "" {
		return ErrTenantIDRequired
	}
	switch t.Status {
	case "", TenantStatusActive, TenantStatusSuspended, TenantStatusDeleted:
		return nil
	default:
		return fmt.Errorf("invalid tenant status %q", t.Status)
	}
}

// Validate checks that the app has the identifiers required for routing.
func (a AgentApp) Validate() error {
	if strings.TrimSpace(a.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(a.AppID) == "" {
		return ErrAppIDRequired
	}
	if a.GrayPercent < 0 || a.GrayPercent > 100 {
		return fmt.Errorf("gray_percent must be between 0 and 100")
	}
	switch a.Status {
	case "", AppStatusActive, AppStatusSuspended, AppStatusDeleted:
		return nil
	default:
		return fmt.Errorf("invalid app status %q", a.Status)
	}
}

// Validate checks that model profile sensitive values are stored by reference.
func (p ModelProfile) Validate() error {
	if strings.TrimSpace(p.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(p.ProfileID) == "" {
		return fmt.Errorf("profile_id is required")
	}
	if err := validateSecretReference("base_url_ref", p.BaseURLRef); err != nil {
		return err
	}
	if err := validateSecretReference("api_key_ref", p.APIKeyRef); err != nil {
		return err
	}
	return nil
}

// Validate checks that a binding has safe routing and secret references.
func (b ChannelBinding) Validate() error {
	if strings.TrimSpace(b.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(b.AppID) == "" {
		return ErrAppIDRequired
	}
	if strings.TrimSpace(b.BindingID) == "" {
		return ErrBindingIDRequired
	}
	if strings.TrimSpace(b.Channel) == "" {
		return ErrChannelRequired
	}
	if strings.TrimSpace(b.AccountID) == "" {
		return ErrAccountIDRequired
	}
	if strings.TrimSpace(b.WebhookPath) == "" {
		return ErrWebhookPathRequired
	}
	if err := validateSecretReference("token_ref", b.TokenRef); err != nil {
		return err
	}
	if err := validateSecretReference("secret_ref", b.SecretRef); err != nil {
		return err
	}
	if err := validateSecretReference("aes_key_ref", b.AESKeyRef); err != nil {
		return err
	}
	switch b.Status {
	case "", BindingStatusActive, BindingStatusDisabled, BindingStatusDeleted:
		return nil
	default:
		return fmt.Errorf("invalid binding status %q", b.Status)
	}
}

// Validate checks that an inbound message has enough identity for routing.
func (m InboundMessage) Validate() error {
	if strings.TrimSpace(m.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(m.AppID) == "" {
		return ErrAppIDRequired
	}
	if strings.TrimSpace(m.Channel) == "" {
		return ErrChannelRequired
	}
	if strings.TrimSpace(m.ChannelAccountID) == "" {
		return ErrAccountIDRequired
	}
	if strings.TrimSpace(m.PlatformMessageID) == "" {
		return ErrPlatformMessageIDRequired
	}
	if strings.TrimSpace(m.ExternalUserID) == "" {
		return ErrExternalUserIDRequired
	}
	switch m.ConversationType {
	case ConversationTypeDM:
		return nil
	case ConversationTypeGroup:
		if strings.TrimSpace(m.ExternalGroupID) == "" {
			return ErrExternalGroupIDRequired
		}
		return nil
	case ConversationTypeThread:
		if strings.TrimSpace(m.ExternalGroupID) == "" {
			return ErrExternalGroupIDRequired
		}
		if strings.TrimSpace(m.ThreadID) == "" {
			return fmt.Errorf("thread_id is required")
		}
		return nil
	case "":
		return ErrConversationTypeRequired
	default:
		return ErrInvalidConversationType
	}
}

// Validate checks that a storage profile uses references for sensitive values.
func (p StorageProfile) Validate() error {
	if strings.TrimSpace(p.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(p.ProfileID) == "" {
		return fmt.Errorf("profile_id is required")
	}
	if err := validateSecretReference("dsn_ref", p.DSNRef); err != nil {
		return err
	}
	if _, err := NormalizeStorageMigrationMode(p.MigrationMode); err != nil {
		return err
	}
	return nil
}

// Validate checks that audit retention and sampling policy is safe to use.
func (p AuditPolicy) Validate() error {
	if strings.TrimSpace(p.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(p.PolicyID) == "" {
		return fmt.Errorf("policy_id is required")
	}
	if p.RetentionDays < 0 {
		return fmt.Errorf("retention_days must be greater than or equal to 0")
	}
	if math.IsNaN(p.SampleRate) || math.IsInf(p.SampleRate, 0) || p.SampleRate < 0 || p.SampleRate > 1 {
		return fmt.Errorf("sample_rate must be between 0 and 1")
	}
	if _, err := NewRedactor(p.RedactionRules...); err != nil {
		return fmt.Errorf("redaction_rules: %w", err)
	}
	return nil
}

func validateSecretReference(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.Contains(value, "=") ||
		hasInlineURLCredential(value) ||
		looksLikeRawSecret(value) {
		return fmt.Errorf("%s: %w", field, ErrInlineSecretRejected)
	}
	return nil
}

func hasInlineURLCredential(value string) bool {
	scheme := strings.Index(value, "://")
	at := strings.Index(value, "@")
	if scheme < 0 || at < 0 || at < scheme {
		return false
	}
	credential := value[scheme+3 : at]
	return strings.Contains(credential, ":")
}

func looksLikeRawSecret(value string) bool {
	lower := strings.ToLower(value)
	for _, prefix := range rawSecretPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	if strings.HasPrefix(lower, "bot") && strings.Contains(value, ":") {
		return true
	}
	if colon := strings.Index(value, ":"); colon > 0 {
		if _, err := strconv.ParseInt(value[:colon], 10, 64); err == nil {
			return true
		}
	}
	if len(value) >= 32 && !strings.ContainsAny(value, "/:.") {
		return true
	}
	return false
}
