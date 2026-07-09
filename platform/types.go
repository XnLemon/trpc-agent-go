//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package platform

import "time"

// TenantStatus is the lifecycle state of a tenant.
type TenantStatus string

const (
	// TenantStatusActive allows normal request processing.
	TenantStatusActive TenantStatus = "active"
	// TenantStatusSuspended rejects new runtime requests while retaining data.
	TenantStatusSuspended TenantStatus = "suspended"
	// TenantStatusDeleted marks a tenant as soft-deleted.
	TenantStatusDeleted TenantStatus = "deleted"
)

// AppStatus is the lifecycle state of an agent application.
type AppStatus string

const (
	// AppStatusActive allows the app to receive runtime traffic.
	AppStatusActive AppStatus = "active"
	// AppStatusSuspended rejects runtime traffic for the app.
	AppStatusSuspended AppStatus = "suspended"
	// AppStatusDeleted marks the app as soft-deleted.
	AppStatusDeleted AppStatus = "deleted"
)

// AppConfigVersionStatus is the lifecycle state of one app configuration version.
type AppConfigVersionStatus string

const (
	// AppConfigVersionStatusDraft is editable and not ready for traffic.
	AppConfigVersionStatusDraft AppConfigVersionStatus = "draft"
	// AppConfigVersionStatusValidated passed offline validation.
	AppConfigVersionStatusValidated AppConfigVersionStatus = "validated"
	// AppConfigVersionStatusReleased is eligible for gray traffic.
	AppConfigVersionStatusReleased AppConfigVersionStatus = "released"
	// AppConfigVersionStatusActive receives normal traffic.
	AppConfigVersionStatusActive AppConfigVersionStatus = "active"
	// AppConfigVersionStatusRollback is retained as the rollback target.
	AppConfigVersionStatusRollback AppConfigVersionStatus = "rollback"
)

// BindingStatus is the lifecycle state of a channel binding.
type BindingStatus string

const (
	// BindingStatusActive allows inbound callbacks through the binding.
	BindingStatusActive BindingStatus = "active"
	// BindingStatusDisabled rejects inbound callbacks through the binding.
	BindingStatusDisabled BindingStatus = "disabled"
	// BindingStatusDeleted marks the binding as soft-deleted.
	BindingStatusDeleted BindingStatus = "deleted"
)

// ConversationType describes the IM conversation scope.
type ConversationType string

const (
	// ConversationTypeDM is a one-to-one conversation.
	ConversationTypeDM ConversationType = "dm"
	// ConversationTypeGroup is a group conversation.
	ConversationTypeGroup ConversationType = "group"
	// ConversationTypeThread is a thread or topic inside a group conversation.
	ConversationTypeThread ConversationType = "thread"
)

// MessageType describes the normalized inbound message kind.
type MessageType string

const (
	// MessageTypeText is a plain text message.
	MessageTypeText MessageType = "text"
	// MessageTypeImage is an image message.
	MessageTypeImage MessageType = "image"
	// MessageTypeFile is a file message.
	MessageTypeFile MessageType = "file"
	// MessageTypeAudio is an audio or voice message.
	MessageTypeAudio MessageType = "audio"
	// MessageTypeVideo is a video message.
	MessageTypeVideo MessageType = "video"
	// MessageTypeEvent is a non-conversational platform event.
	MessageTypeEvent MessageType = "event"
	// MessageTypeUnknown is an unsupported or unknown message type.
	MessageTypeUnknown MessageType = "unknown"
)

// ContentPartType describes one normalized content part.
type ContentPartType string

const (
	// ContentPartTypeText carries text content.
	ContentPartTypeText ContentPartType = "text"
	// ContentPartTypeImage carries an image artifact reference.
	ContentPartTypeImage ContentPartType = "image"
	// ContentPartTypeFile carries a file artifact reference.
	ContentPartTypeFile ContentPartType = "file"
	// ContentPartTypeAudio carries an audio artifact reference.
	ContentPartTypeAudio ContentPartType = "audio"
	// ContentPartTypeVideo carries a video artifact reference.
	ContentPartTypeVideo ContentPartType = "video"
	// ContentPartTypeLocation carries a location payload.
	ContentPartTypeLocation ContentPartType = "location"
	// ContentPartTypeUnknown carries unsupported content metadata.
	ContentPartTypeUnknown ContentPartType = "unknown"
)

// OutboundMessageKind describes the kind of payload sent to an IM platform.
type OutboundMessageKind string

const (
	// OutboundMessageKindText sends plain text.
	OutboundMessageKindText OutboundMessageKind = "text"
	// OutboundMessageKindMarkdown sends markdown when the channel supports it.
	OutboundMessageKindMarkdown OutboundMessageKind = "markdown"
	// OutboundMessageKindCard sends a structured card.
	OutboundMessageKindCard OutboundMessageKind = "card"
	// OutboundMessageKindImage sends an image.
	OutboundMessageKindImage OutboundMessageKind = "image"
	// OutboundMessageKindFile sends a file.
	OutboundMessageKindFile OutboundMessageKind = "file"
	// OutboundMessageKindStatus sends an execution status update.
	OutboundMessageKindStatus OutboundMessageKind = "status"
)

// IdempotencyStatus is the state of one inbound platform message.
type IdempotencyStatus string

const (
	// IdempotencyStatusReceived records that the callback was accepted.
	IdempotencyStatusReceived IdempotencyStatus = "received"
	// IdempotencyStatusProcessing records that the runner is still executing.
	IdempotencyStatusProcessing IdempotencyStatus = "processing"
	// IdempotencyStatusCompleted records that the runner finished and must not rerun.
	IdempotencyStatusCompleted IdempotencyStatus = "completed"
	// IdempotencyStatusReplyFailed records that only outbound delivery failed.
	IdempotencyStatusReplyFailed IdempotencyStatus = "reply_failed"
	// IdempotencyStatusDeadLetter records an item requiring manual replay.
	IdempotencyStatusDeadLetter IdempotencyStatus = "dead_letter"
)

// OutboundStatus is the delivery state of one outbound IM message.
type OutboundStatus string

const (
	// OutboundStatusPending is waiting for delivery.
	OutboundStatusPending OutboundStatus = "pending"
	// OutboundStatusSent was delivered to the platform.
	OutboundStatusSent OutboundStatus = "sent"
	// OutboundStatusFailed failed and may be retried.
	OutboundStatusFailed OutboundStatus = "failed"
	// OutboundStatusDeadLetter failed permanently or exhausted retries.
	OutboundStatusDeadLetter OutboundStatus = "dead_letter"
)

// DangerousToolAction is the default action for high-risk tools.
type DangerousToolAction string

const (
	// DangerousToolActionDeny blocks high-risk tools.
	DangerousToolActionDeny DangerousToolAction = "deny"
	// DangerousToolActionAsk requires approval before high-risk tools execute.
	DangerousToolActionAsk DangerousToolAction = "ask"
	// DangerousToolActionAllowWithAudit allows high-risk tools with audit.
	DangerousToolActionAllowWithAudit DangerousToolAction = "allow_with_audit"
)

// Tenant is the top-level isolation boundary.
type Tenant struct {
	TenantID                string
	Name                    string
	Status                  TenantStatus
	Region                  string
	QuotaJSON               string
	DefaultStorageProfileID string
	AuditPolicyID           string
	CreatedAt               time.Time
	UpdatedAt               time.Time
	DeletedAt               *time.Time
}

// AgentApp is a tenant-owned agent application configuration.
type AgentApp struct {
	TenantID         string
	AppID            string
	AppName          string
	AgentName        string
	InstructionRef   string
	ModelProfileID   string
	ToolPolicyID     string
	StorageProfileID string
	MemoryProfileID  string
	ReleaseVersion   string
	GrayPercent      int
	Status           AppStatus
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AppConfigVersion stores one deployable app configuration bundle.
type AppConfigVersion struct {
	TenantID         string
	AppID            string
	Version          string
	ConfigBundleJSON string
	Checksum         string
	Status           AppConfigVersionStatus
	GrayPercent      int
	CreatedBy        string
	CreatedAt        time.Time
	ActivatedAt      *time.Time
}

// ModelProfile stores model provider configuration references.
type ModelProfile struct {
	TenantID          string
	ProfileID         string
	Provider          string
	Model             string
	BaseURLRef        string
	APIKeyRef         string
	TimeoutMS         int
	MaxTokens         int
	Temperature       float64
	FallbackProfileID string
	CostPolicyJSON    string
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ToolPolicy stores tenant and app-level tool governance.
type ToolPolicy struct {
	TenantID                string
	PolicyID                string
	AppID                   string
	ToolWhitelist           []string
	ToolDenylist            []string
	DangerousToolAction     DangerousToolAction
	ApprovalChannel         string
	ArgumentRedactionRules  []string
	NetworkPolicyJSON       string
	FilesystemPolicyJSON    string
	PlatformDenylist        []string
	HighRiskTools           []string
	ToolBudgetRemainingJSON string
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// ChannelLimits stores configurable channel capability and limit values.
type ChannelLimits struct {
	MaxTextLength      int
	CallbackACKTimeout time.Duration
	FileMaxBytes       int64
	RateLimitQPS       int
	Burst              int
	SupportsAsyncReply bool
	SupportsEdit       bool
	SupportsCardUpdate bool
	RetryMaxAttempts   int
	RetryBackoff       string
}

// ChannelBinding maps one external IM account to one tenant app.
type ChannelBinding struct {
	TenantID        string
	BindingID       string
	AppID           string
	Channel         string
	AccountID       string
	WebhookPath     string
	TokenRef        string
	SecretRef       string
	AESKeyRef       string
	AllowedUsers    []string
	AllowedGroups   []string
	RequiredMention bool
	Status          BindingStatus
	ChannelLimits   ChannelLimits
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// StorageProfile stores backend choices for a tenant app.
type StorageProfile struct {
	TenantID         string
	ProfileID        string
	SessionBackend   string
	MemoryBackend    string
	SummaryBackend   string
	ArtifactBackend  string
	KnowledgeBackend string
	AuditBackend     string
	DSNRef           string
	Namespace        string
	TTLJSON          string
	MigrationMode    string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AuditPolicy stores audit retention and export settings.
type AuditPolicy struct {
	TenantID              string
	PolicyID              string
	RetentionDays         int
	SampleRate            float64
	FullAuditForRiskyTool bool
	RedactionRules        []string
	ExportSink            string
	ComplianceLevel       string
}

// IMUserMapping maps an external IM identity to an internal identity.
type IMUserMapping struct {
	TenantID       string
	Channel        string
	ExternalUserID string
	InternalUserID string
	DisplayName    string
	Roles          []string
	Status         string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ContentPart is one normalized part of an inbound message.
type ContentPart struct {
	Type         ContentPartType
	Text         string
	FileRef      string
	MIMEType     string
	SizeBytes    int64
	SHA256       string
	MetadataJSON string
}

// InboundMessage is the normalized message consumed by a gateway.
type InboundMessage struct {
	TenantID            string
	AppID               string
	BindingID           string
	Channel             string
	ChannelAccountID    string
	PlatformMessageID   string
	ExternalUserID      string
	ExternalGroupID     string
	ThreadID            string
	ConversationType    ConversationType
	MessageType         MessageType
	ContentParts        []ContentPart
	RawEventType        string
	ReceivedAt          time.Time
	SignatureStatus     string
	TraceContext        map[string]string
	RequiredMentionSeen bool
}

// OutboundMessage is the normalized payload delivered to an IM platform.
type OutboundMessage struct {
	TenantID                 string
	BindingID                string
	Channel                  string
	SessionID                string
	ReplyToPlatformMessageID string
	Kind                     OutboundMessageKind
	Content                  string
	FileRef                  string
	Sequence                 int
	DedupKey                 string
	RetryPolicy              string
	TraceID                  string
}

// IdempotencyRecord stores duplicate delivery state for an inbound message.
type IdempotencyRecord struct {
	TenantID          string
	Channel           string
	AccountID         string
	PlatformMessageID string
	IdempotencyKey    string
	RequestID         string
	SessionID         string
	Status            IdempotencyStatus
	FirstSeenAt       time.Time
	UpdatedAt         time.Time
	ResultRef         string
}

// AuditRecord stores governance and troubleshooting metadata.
type AuditRecord struct {
	TenantID          string
	AuditID           string
	AppID             string
	Channel           string
	BindingID         string
	UserID            string
	InternalUserID    string
	UserIDHash        string
	SessionID         string
	MessageID         string
	RequestID         string
	AgentName         string
	ModelName         string
	ToolName          string
	Decision          string
	DecisionReason    string
	LatencyMS         int64
	ErrorType         string
	Cost              float64
	TokenUsageJSON    string
	TraceID           string
	RedactedDetailRef string
	RedactionVersion  string
	CreatedAt         time.Time
}

// UsageRecord stores post-run token and cost accounting dimensions.
type UsageRecord struct {
	TenantID         string
	AppID            string
	UserIDHash       string
	SessionID        string
	RequestID        string
	ModelName        string
	ToolName         string
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	ModelUnitPrice   float64
	ModelCost        float64
	ToolCost         float64
	TotalCost        float64
	TraceID          string
	CreatedAt        time.Time
}
