//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode"
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

func validateRoutingIdentifier(field, value string, requiredErr error) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return requiredErr
	}
	if trimmed != value {
		return fmt.Errorf("%s must not contain leading or trailing whitespace", field)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s must not contain control characters", field)
		}
	}
	return nil
}

// Validate checks that the tenant can be used as an isolation boundary.
func (t Tenant) Validate() error {
	if err := validateRoutingIdentifier("tenant_id", t.TenantID, ErrTenantIDRequired); err != nil {
		return err
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
	if err := validateRoutingIdentifier("tenant_id", a.TenantID, ErrTenantIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("app_id", a.AppID, ErrAppIDRequired); err != nil {
		return err
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

// Validate checks that an app config version is safe to store and route.
func (v AppConfigVersion) Validate() error {
	if strings.TrimSpace(v.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(v.AppID) == "" {
		return ErrAppIDRequired
	}
	if strings.TrimSpace(v.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if strings.TrimSpace(v.ConfigBundleJSON) == "" {
		return fmt.Errorf("config_bundle_json is required")
	}
	if !json.Valid([]byte(v.ConfigBundleJSON)) {
		return fmt.Errorf("config_bundle_json must be valid json")
	}
	if err := validateConfigBundleJSON(v.ConfigBundleJSON); err != nil {
		return err
	}
	if strings.TrimSpace(v.Checksum) == "" {
		return fmt.Errorf("checksum is required")
	}
	if err := validateAuditRedactedText("checksum", v.Checksum); err != nil {
		return err
	}
	if v.GrayPercent < 0 || v.GrayPercent > 100 {
		return fmt.Errorf("gray_percent must be between 0 and 100")
	}
	switch v.Status {
	case AppConfigVersionStatusDraft,
		AppConfigVersionStatusValidated,
		AppConfigVersionStatusReleased,
		AppConfigVersionStatusActive,
		AppConfigVersionStatusRollback:
		return nil
	case "":
		return fmt.Errorf("status is required")
	default:
		return fmt.Errorf("invalid app config version status %q", v.Status)
	}
}

func validateConfigBundleJSON(bundle string) error {
	var value any
	if err := json.Unmarshal([]byte(bundle), &value); err != nil {
		return fmt.Errorf("config_bundle_json must be valid json")
	}
	return validateConfigBundleValue("config_bundle_json", "", value)
}

func validateConfigBundleValue(path, key string, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, childValue := range typed {
			childPath := path + "." + childKey
			if err := validateConfigBundleValue(childPath, childKey, childValue); err != nil {
				return err
			}
		}
	case []any:
		for i, childValue := range typed {
			childPath := fmt.Sprintf("%s[%d]", path, i)
			if err := validateConfigBundleValue(childPath, key, childValue); err != nil {
				return err
			}
		}
	case string:
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(key)), "_ref") {
			if err := validateSecretReference(path, typed); err != nil {
				return err
			}
			return nil
		}
		if err := validateAuditRedactedText(path, typed); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks that model profile sensitive values are stored by reference.
func (p ModelProfile) Validate() error {
	if err := validateRoutingIdentifier("tenant_id", p.TenantID, ErrTenantIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("profile_id", p.ProfileID, fmt.Errorf("profile_id is required")); err != nil {
		return err
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
	if err := validateRoutingIdentifier("tenant_id", b.TenantID, ErrTenantIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("app_id", b.AppID, ErrAppIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("binding_id", b.BindingID, ErrBindingIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("channel", b.Channel, ErrChannelRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("account_id", b.AccountID, ErrAccountIDRequired); err != nil {
		return err
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
	if err := validateRoutingIdentifier("tenant_id", m.TenantID, ErrTenantIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("app_id", m.AppID, ErrAppIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("binding_id", m.BindingID, ErrBindingIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("channel", m.Channel, ErrChannelRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("channel_account_id", m.ChannelAccountID, ErrAccountIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("platform_message_id", m.PlatformMessageID, ErrPlatformMessageIDRequired); err != nil {
		return err
	}
	if err := validateInboundMessageType(m); err != nil {
		return err
	}
	if m.MessageType == MessageTypeEvent {
		return nil
	}
	if err := validateRoutingIdentifier("external_user_id", m.ExternalUserID, ErrExternalUserIDRequired); err != nil {
		return err
	}
	switch m.ConversationType {
	case ConversationTypeDM:
		return nil
	case ConversationTypeGroup:
		if err := validateRoutingIdentifier("external_group_id", m.ExternalGroupID, ErrExternalGroupIDRequired); err != nil {
			return err
		}
		return nil
	case ConversationTypeThread:
		if err := validateRoutingIdentifier("external_group_id", m.ExternalGroupID, ErrExternalGroupIDRequired); err != nil {
			return err
		}
		if err := validateRoutingIdentifier("thread_id", m.ThreadID, fmt.Errorf("thread_id is required")); err != nil {
			return err
		}
		return nil
	case "":
		return ErrConversationTypeRequired
	default:
		return ErrInvalidConversationType
	}
}

func validateInboundMessageType(m InboundMessage) error {
	switch m.MessageType {
	case MessageTypeText, MessageTypeImage, MessageTypeFile, MessageTypeAudio, MessageTypeVideo:
		return nil
	case MessageTypeEvent:
		return validateRoutingIdentifier("raw_event_type", m.RawEventType, fmt.Errorf("raw_event_type is required"))
	case "":
		return fmt.Errorf("message_type is required")
	default:
		return fmt.Errorf("invalid message_type %q", m.MessageType)
	}
}

// Validate checks that a storage profile uses references for sensitive values.
func (p StorageProfile) Validate() error {
	if err := validateRoutingIdentifier("tenant_id", p.TenantID, ErrTenantIDRequired); err != nil {
		return err
	}
	if err := validateRoutingIdentifier("profile_id", p.ProfileID, fmt.Errorf("profile_id is required")); err != nil {
		return err
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

// Validate checks that an audit record has required identity and no raw secret detail.
func (r AuditRecord) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(r.AuditID) == "" {
		return fmt.Errorf("audit_id is required")
	}
	if r.LatencyMS < 0 {
		return fmt.Errorf("latency_ms must be greater than or equal to 0")
	}
	if math.IsNaN(r.Cost) || math.IsInf(r.Cost, 0) || r.Cost < 0 {
		return fmt.Errorf("cost must be greater than or equal to 0")
	}
	if err := validateAuditRedactedText("decision_reason", r.DecisionReason); err != nil {
		return err
	}
	if err := validateAuditRedactedText("error_type", r.ErrorType); err != nil {
		return err
	}
	if err := validateAuditRedactedText("token_usage_json", r.TokenUsageJSON); err != nil {
		return err
	}
	if err := validateAuditRedactedText("redacted_detail_ref", r.RedactedDetailRef); err != nil {
		return err
	}
	return nil
}

// Validate checks that a usage record has required identity and safe accounting values.
func (r UsageRecord) Validate() error {
	if strings.TrimSpace(r.TenantID) == "" {
		return ErrTenantIDRequired
	}
	if strings.TrimSpace(r.AppID) == "" {
		return ErrAppIDRequired
	}
	for field, value := range map[string]string{
		"user_id_hash": r.UserIDHash,
		"session_id":   r.SessionID,
		"request_id":   r.RequestID,
		"model_name":   r.ModelName,
		"tool_name":    r.ToolName,
		"trace_id":     r.TraceID,
	} {
		if err := validateAuditRedactedText(field, value); err != nil {
			return err
		}
	}
	if r.PromptTokens < 0 ||
		r.CompletionTokens < 0 ||
		r.CachedTokens < 0 {
		return fmt.Errorf("usage token values must be non-negative")
	}
	if !isFiniteNonNegative(r.ModelUnitPrice) {
		return fmt.Errorf("model_unit_price must be finite and non-negative")
	}
	if !isFiniteNonNegative(r.ModelCost) {
		return fmt.Errorf("model_cost must be finite and non-negative")
	}
	if !isFiniteNonNegative(r.ToolCost) {
		return fmt.Errorf("tool_cost must be finite and non-negative")
	}
	if !isFiniteNonNegative(r.TotalCost) {
		return fmt.Errorf("total_cost must be finite and non-negative")
	}
	return nil
}

func validateAuditRedactedText(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	redactor, err := NewRedactor()
	if err != nil {
		return fmt.Errorf("%s: redactor unavailable: %w", field, err)
	}
	if redactor.Redact(value) != value {
		return fmt.Errorf("%s contains unredacted sensitive content", field)
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
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User == nil {
		return false
	}
	if parsed.User.Username() != "" {
		return true
	}
	_, hasPassword := parsed.User.Password()
	return hasPassword
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
