//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package artifactstore

import "errors"

var (
	// ErrTenantIDRequired indicates that the service lacks a tenant boundary.
	ErrTenantIDRequired = errors.New("artifactstore tenant_id is required")
	// ErrNamespaceRequired indicates that the storage namespace is missing.
	ErrNamespaceRequired = errors.New("artifactstore namespace is required")
	// ErrMetadataStoreRequired indicates that metadata storage was not configured.
	ErrMetadataStoreRequired = errors.New("artifactstore metadata store is required")
	// ErrObjectStoreRequired indicates that object storage was not configured.
	ErrObjectStoreRequired = errors.New("artifactstore object store is required")
	// ErrOutsideTenantScope indicates that a key or query escapes the tenant scope.
	ErrOutsideTenantScope = errors.New("artifactstore key outside tenant scope")
	// ErrEmptySessionInfo indicates that required session fields are missing.
	ErrEmptySessionInfo = errors.New("artifactstore session info fields cannot be empty")
	// ErrEmptyFilename indicates that the filename is empty.
	ErrEmptyFilename = errors.New("artifactstore filename cannot be empty")
	// ErrInvalidFilename indicates that the filename contains unsafe path data.
	ErrInvalidFilename = errors.New("artifactstore filename contains invalid characters")
	// ErrNilArtifact indicates that the artifact payload is nil.
	ErrNilArtifact = errors.New("artifactstore artifact cannot be nil")
	// ErrObjectNotFound indicates that object content is missing.
	ErrObjectNotFound = errors.New("artifactstore object not found")
	// ErrObjectConflict indicates that an object ID already contains different bytes.
	ErrObjectConflict = errors.New("artifactstore object conflict")
	// ErrVersionConflict indicates that another writer committed the same version.
	ErrVersionConflict = errors.New("artifactstore version conflict")
	// ErrMetadataReservationNotFound indicates that a pending upload record is missing.
	ErrMetadataReservationNotFound = errors.New("artifactstore metadata reservation not found")
	// ErrArtifactWriteInProgress indicates that deletion raced with a pending upload.
	ErrArtifactWriteInProgress = errors.New("artifactstore write in progress")
)
