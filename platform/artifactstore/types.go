//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package artifactstore

import (
	"context"
	"time"
)

// MetadataReservationBuilder builds a pending metadata record for the atomically
// allocated artifact version.
type MetadataReservationBuilder func(version int) (MetadataRecord, error)

// MetadataStore stores queryable artifact metadata without embedding object bytes.
type MetadataStore interface {
	// Reserve atomically allocates the next scoped version and stores its
	// pending metadata reservation.
	Reserve(
		ctx context.Context,
		query MetadataQuery,
		build MetadataReservationBuilder,
	) (MetadataRecord, error)
	// Put stores one explicit metadata record and returns ErrVersionConflict
	// when the scoped version already exists.
	Put(ctx context.Context, record MetadataRecord) error
	// Query hides pending uploads and deleting tombstones unless requested.
	Query(ctx context.Context, query MetadataQuery) ([]MetadataRecord, error)
	// Activate publishes a pending version after its object upload succeeds.
	Activate(ctx context.Context, query MetadataQuery, objectID string) error
	// MarkDeleting atomically hides matching records. Repeated calls must also
	// return existing tombstones so failed object cleanup can be retried. It
	// returns ErrArtifactWriteInProgress rather than changing pending uploads,
	// unless AllowPendingTransition is set for an exact owner cleanup.
	MarkDeleting(ctx context.Context, query MetadataQuery) ([]MetadataRecord, error)
	// Delete permanently removes matching metadata after object cleanup.
	Delete(ctx context.Context, query MetadataQuery) ([]MetadataRecord, error)
}

// ObjectStore stores artifact object content addressed by opaque object IDs.
type ObjectStore interface {
	// Put must be retry-safe: storing the same object ID with identical bytes is
	// idempotent, while storing different bytes for an existing ID returns
	// ErrObjectConflict without changing the committed object.
	Put(ctx context.Context, object ObjectRecord) error
	Get(ctx context.Context, objectID string) ([]byte, error)
	// Delete must be idempotent and treat an already-missing object as success.
	Delete(ctx context.Context, objectID string) error
}

// MetadataStatus describes whether an artifact version is visible or pending cleanup.
type MetadataStatus string

const (
	// MetadataStatusPending reserves a version while its object upload commits.
	MetadataStatusPending MetadataStatus = "pending"
	// MetadataStatusActive makes the artifact version visible to normal queries.
	MetadataStatusActive MetadataStatus = "active"
	// MetadataStatusDeleting hides the version while object cleanup is pending.
	MetadataStatusDeleting MetadataStatus = "deleting"
)

// ServiceConfig wires the metadata and object stores for one tenant namespace.
type ServiceConfig struct {
	TenantID      string
	Namespace     string
	MetadataStore MetadataStore
	ObjectStore   ObjectStore
	MaxAttempts   int
}

// MetadataRecord describes one artifact version.
type MetadataRecord struct {
	TenantID       string
	AppName        string
	UserID         string
	SessionID      string
	Filename       string
	Version        int
	MimeType       string
	SizeBytes      int64
	SHA256         string
	AttachmentKind string
	ContentRef     string
	// ObjectID is an opaque backend identifier, not a raw object key.
	// Store implementations must not encode secrets or credentials in it.
	ObjectID   string
	ArtifactID string
	Status     MetadataStatus
	// ReservationOwner identifies the writer that owns a pending reservation.
	ReservationOwner string
	// ReservationExpiresAt is the time after which a pending reservation may be
	// reclaimed by cleanup or delete recovery.
	ReservationExpiresAt time.Time
}

// MetadataQuery filters artifact metadata records. Empty string fields are not
// applied, while Version filters only when non-nil.
type MetadataQuery struct {
	TenantID  string
	AppName   string
	UserID    string
	SessionID string
	Filename  string
	Version   *int
	ObjectID  string
	// ReservationOwner restricts pending reservation transitions to the writer
	// that created them.
	ReservationOwner string
	// IncludePending includes upload reservations retained for safe cleanup.
	IncludePending bool
	// IncludeDeleting includes tombstones retained for retryable object cleanup.
	IncludeDeleting bool
	// AllowPendingTransition permits an exact ObjectID owner cleanup to cancel
	// its own pending upload.
	AllowPendingTransition bool
}

// ObjectRecord contains the bytes written to object storage.
type ObjectRecord struct {
	ObjectID  string
	TenantID  string
	Data      []byte
	MimeType  string
	SizeBytes int64
	SHA256    string
}
