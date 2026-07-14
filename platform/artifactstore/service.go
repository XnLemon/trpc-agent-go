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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"trpc.group/trpc-go/trpc-agent-go/artifact"
)

const (
	defaultContentType        = "application/octet-stream"
	userNamespace             = "user:"
	reservationTTL            = 15 * time.Minute
	cleanupReservationTimeout = 5 * time.Second
)

var _ artifact.Service = (*Service)(nil)

// Service implements artifact.Service using split metadata and object stores.
type Service struct {
	tenantID      string
	namespace     string
	metadataStore MetadataStore
	objectStore   ObjectStore
	maxAttempts   int
}

// New creates a tenant-scoped artifact service.
func New(config ServiceConfig) (*Service, error) {
	tenantID := strings.TrimSpace(config.TenantID)
	if tenantID == "" {
		return nil, ErrTenantIDRequired
	}
	namespace := strings.TrimRight(strings.TrimSpace(config.Namespace), `/\|:`)
	if namespace == "" {
		return nil, ErrNamespaceRequired
	}
	if !namespaceContainsSegment(namespace, tenantID) {
		return nil, ErrOutsideTenantScope
	}
	if config.MetadataStore == nil {
		return nil, ErrMetadataStoreRequired
	}
	if config.ObjectStore == nil {
		return nil, ErrObjectStoreRequired
	}
	maxAttempts := config.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 1
	}
	if maxAttempts < 0 {
		return nil, fmt.Errorf("artifactstore max attempts cannot be negative")
	}
	return &Service{
		tenantID:      tenantID,
		namespace:     namespace,
		metadataStore: config.MetadataStore,
		objectStore:   config.ObjectStore,
		maxAttempts:   maxAttempts,
	}, nil
}

// SaveArtifact reserves metadata, uploads object bytes, then publishes the version.
func (s *Service) SaveArtifact(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
	filename string,
	art *artifact.Artifact,
) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := s.validateSessionInfo(sessionInfo); err != nil {
		return 0, err
	}
	if err := validateFilename(filename); err != nil {
		return 0, err
	}
	if art == nil {
		return 0, ErrNilArtifact
	}

	data := append([]byte(nil), art.Data...)
	digest := sha256.Sum256(data)
	sha := hex.EncodeToString(digest[:])
	mimeType := strings.TrimSpace(art.MimeType)
	if mimeType == "" {
		mimeType = defaultContentType
	}
	artifactID := makeArtifactID(s.tenantID, sessionInfo, filename)
	query := s.metadataQuery(sessionInfo, filename)
	query.IncludePending = true
	query.IncludeDeleting = true
	record, err := s.metadataStore.Reserve(
		ctx,
		query,
		func(version int) (MetadataRecord, error) {
			objectID, err := makeObjectID(artifactID, version, sha)
			if err != nil {
				return MetadataRecord{}, err
			}
			return MetadataRecord{
				TenantID:             s.tenantID,
				AppName:              sessionInfo.AppName,
				UserID:               sessionInfo.UserID,
				SessionID:            metadataSessionID(sessionInfo, filename),
				Filename:             filename,
				Version:              version,
				MimeType:             mimeType,
				SizeBytes:            int64(len(data)),
				SHA256:               sha,
				AttachmentKind:       attachmentKind(mimeType),
				ContentRef:           makeContentRef(artifactID, version),
				ObjectID:             objectID,
				ArtifactID:           artifactID,
				Status:               MetadataStatusPending,
				ReservationOwner:     objectID,
				ReservationExpiresAt: time.Now().Add(reservationTTL),
			}, nil
		},
	)
	if err != nil {
		return 0, fmt.Errorf("reserve artifact metadata: %w", err)
	}
	object := ObjectRecord{
		ObjectID:  record.ObjectID,
		TenantID:  s.tenantID,
		Data:      data,
		MimeType:  mimeType,
		SizeBytes: int64(len(data)),
		SHA256:    sha,
	}
	if err := s.putObjectWithRetry(ctx, object); err != nil {
		return 0, errors.Join(err, s.cleanupReservedArtifact(ctx, record))
	}
	activateQuery := s.metadataQuery(sessionInfo, filename)
	activateQuery.Version = &record.Version
	activateQuery.IncludePending = true
	activateQuery.ReservationOwner = record.ReservationOwner
	if err := s.metadataStore.Activate(ctx, activateQuery, record.ObjectID); err != nil {
		return 0, errors.Join(
			fmt.Errorf("activate artifact metadata: %w", err),
			s.cleanupReservedArtifact(ctx, record),
		)
	}
	return record.Version, nil
}

// LoadArtifact loads object bytes using metadata as the authority.
func (s *Service) LoadArtifact(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
	filename string,
	version *int,
) (*artifact.Artifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	record, err := s.Metadata(ctx, sessionInfo, filename, version)
	if err != nil || record == nil {
		return nil, err
	}
	data, err := s.objectStore.Get(ctx, record.ObjectID)
	if err != nil {
		return nil, fmt.Errorf("get artifact object: %w", err)
	}
	return &artifact.Artifact{
		Data:     data,
		MimeType: record.MimeType,
		Name:     filename,
	}, nil
}

// Metadata returns one metadata record for the requested artifact version.
func (s *Service) Metadata(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
	filename string,
	version *int,
) (*MetadataRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.validateSessionInfo(sessionInfo); err != nil {
		return nil, err
	}
	if err := validateFilename(filename); err != nil {
		return nil, err
	}
	query := s.metadataQuery(sessionInfo, filename)
	query.Version = version
	records, err := s.metadataStore.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query artifact metadata: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	sortMetadata(records)
	record := records[len(records)-1]
	return &record, nil
}

// ListArtifactKeys lists artifact filenames within the session boundary.
func (s *Service) ListArtifactKeys(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.validateSessionInfo(sessionInfo); err != nil {
		return nil, err
	}
	records, err := s.metadataStore.Query(ctx, MetadataQuery{
		TenantID:  s.tenantID,
		AppName:   sessionInfo.AppName,
		UserID:    sessionInfo.UserID,
		SessionID: sessionInfo.SessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("query artifact metadata: %w", err)
	}
	userRecords, err := s.metadataStore.Query(ctx, MetadataQuery{
		TenantID:  s.tenantID,
		AppName:   sessionInfo.AppName,
		UserID:    sessionInfo.UserID,
		SessionID: userArtifactSessionID,
	})
	if err != nil {
		return nil, fmt.Errorf("query user artifact metadata: %w", err)
	}
	records = append(records, userRecords...)
	names := make(map[string]struct{}, len(records))
	for _, record := range records {
		names[record.Filename] = struct{}{}
	}
	filenames := make([]string, 0, len(names))
	for filename := range names {
		filenames = append(filenames, filename)
	}
	sort.Strings(filenames)
	return filenames, nil
}

// DeleteArtifact removes all metadata and object versions for an artifact.
func (s *Service) DeleteArtifact(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
	filename string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.validateSessionInfo(sessionInfo); err != nil {
		return err
	}
	if err := validateFilename(filename); err != nil {
		return err
	}
	query := s.metadataQuery(sessionInfo, filename)
	records, err := s.metadataStore.MarkDeleting(ctx, query)
	if err != nil {
		return fmt.Errorf("mark artifact metadata deleting: %w", err)
	}
	if len(records) == 0 {
		return nil
	}
	return s.cleanupMetadataRecords(ctx, query, records)
}

func (s *Service) cleanupReservedArtifact(
	ctx context.Context,
	record MetadataRecord,
) error {
	cleanupCtx, cancel := context.WithTimeout(
		context.WithoutCancel(ctx),
		cleanupReservationTimeout,
	)
	defer cancel()
	version := record.Version
	query := MetadataQuery{
		TenantID:               record.TenantID,
		AppName:                record.AppName,
		UserID:                 record.UserID,
		SessionID:              record.SessionID,
		Filename:               record.Filename,
		Version:                &version,
		ObjectID:               record.ObjectID,
		ReservationOwner:       record.ReservationOwner,
		IncludePending:         true,
		IncludeDeleting:        true,
		AllowPendingTransition: true,
	}
	records, err := s.metadataStore.MarkDeleting(cleanupCtx, query)
	if err != nil {
		return fmt.Errorf("mark reserved artifact deleting: %w", err)
	}
	if len(records) == 0 {
		return ErrMetadataReservationNotFound
	}
	return s.cleanupMetadataRecords(cleanupCtx, query, records)
}

func (s *Service) cleanupMetadataRecords(
	ctx context.Context,
	query MetadataQuery,
	records []MetadataRecord,
) error {
	var cleanupErrs []error
	for _, record := range records {
		if err := s.objectStore.Delete(ctx, record.ObjectID); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf(
				"delete artifact object %q: %w",
				record.ObjectID,
				err,
			))
			continue
		}
		version := record.Version
		deleteQuery := query
		deleteQuery.Version = &version
		deleteQuery.ObjectID = record.ObjectID
		deleteQuery.IncludePending = true
		deleteQuery.IncludeDeleting = true
		if _, err := s.metadataStore.Delete(ctx, deleteQuery); err != nil {
			cleanupErrs = append(cleanupErrs, fmt.Errorf(
				"delete artifact metadata version %d: %w",
				version,
				err,
			))
		}
	}
	return errors.Join(cleanupErrs...)
}

// ListVersions lists all versions of an artifact.
func (s *Service) ListVersions(
	ctx context.Context,
	sessionInfo artifact.SessionInfo,
	filename string,
) ([]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.validateSessionInfo(sessionInfo); err != nil {
		return nil, err
	}
	if err := validateFilename(filename); err != nil {
		return nil, err
	}
	records, err := s.metadataStore.Query(ctx, s.metadataQuery(sessionInfo, filename))
	if err != nil {
		return nil, fmt.Errorf("query artifact metadata: %w", err)
	}
	versionSet := make(map[int]struct{}, len(records))
	for _, record := range records {
		versionSet[record.Version] = struct{}{}
	}
	versions := make([]int, 0, len(versionSet))
	for version := range versionSet {
		versions = append(versions, version)
	}
	sort.Ints(versions)
	return versions, nil
}

func (s *Service) putObjectWithRetry(ctx context.Context, object ObjectRecord) error {
	var lastErr error
	for attempt := 0; attempt < s.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.objectStore.Put(ctx, object); err != nil {
			if errors.Is(err, ErrObjectConflict) {
				return err
			}
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("put artifact object after %d attempts: %w", s.maxAttempts, lastErr)
}

func (s *Service) metadataQuery(sessionInfo artifact.SessionInfo, filename string) MetadataQuery {
	return MetadataQuery{
		TenantID:  s.tenantID,
		AppName:   sessionInfo.AppName,
		UserID:    sessionInfo.UserID,
		SessionID: metadataSessionID(sessionInfo, filename),
		Filename:  filename,
	}
}

func (s *Service) validateSessionInfo(info artifact.SessionInfo) error {
	if strings.TrimSpace(info.AppName) == "" ||
		strings.TrimSpace(info.UserID) == "" ||
		strings.TrimSpace(info.SessionID) == "" {
		return ErrEmptySessionInfo
	}
	if hasLeadingOrTrailingSpace(info.AppName) ||
		hasLeadingOrTrailingSpace(info.UserID) ||
		hasLeadingOrTrailingSpace(info.SessionID) {
		return ErrEmptySessionInfo
	}
	if containsControl(info.AppName) ||
		containsControl(info.UserID) ||
		containsControl(info.SessionID) {
		return ErrEmptySessionInfo
	}
	if info.SessionID == userArtifactSessionID {
		return ErrEmptySessionInfo
	}
	prefix := s.namespace + "/"
	if !strings.HasPrefix(info.AppName, prefix) || strings.TrimSpace(strings.TrimPrefix(info.AppName, prefix)) == "" {
		return ErrOutsideTenantScope
	}
	return nil
}

func validateFilename(filename string) error {
	if strings.TrimSpace(filename) == "" {
		return ErrEmptyFilename
	}
	if hasLeadingOrTrailingSpace(filename) ||
		strings.Contains(filename, "\\") ||
		strings.Contains(filename, "\x00") ||
		containsControl(filename) {
		return ErrInvalidFilename
	}
	for _, segment := range strings.Split(filename, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return ErrInvalidFilename
		}
	}
	return nil
}

const userArtifactSessionID = "user"

func metadataSessionID(info artifact.SessionInfo, filename string) string {
	if strings.HasPrefix(filename, userNamespace) {
		return userArtifactSessionID
	}
	return info.SessionID
}

func nextVersion(records []MetadataRecord) int {
	if len(records) == 0 {
		return 0
	}
	version := 0
	for _, record := range records {
		if record.Version >= version {
			version = record.Version + 1
		}
	}
	return version
}

func sortMetadata(records []MetadataRecord) {
	sort.Slice(records, func(i, j int) bool {
		left := records[i]
		right := records[j]
		if left.TenantID != right.TenantID {
			return left.TenantID < right.TenantID
		}
		if left.AppName != right.AppName {
			return left.AppName < right.AppName
		}
		if left.UserID != right.UserID {
			return left.UserID < right.UserID
		}
		if left.SessionID != right.SessionID {
			return left.SessionID < right.SessionID
		}
		if left.Filename != right.Filename {
			return left.Filename < right.Filename
		}
		return left.Version < right.Version
	})
}

func attachmentKind(mimeType string) string {
	switch {
	case strings.HasPrefix(mimeType, "image/"):
		return "image"
	case strings.HasPrefix(mimeType, "audio/"):
		return "audio"
	case strings.HasPrefix(mimeType, "video/"):
		return "video"
	default:
		return "file"
	}
}

func makeArtifactID(tenantID string, info artifact.SessionInfo, filename string) string {
	return "art_" + scopedHash(tenantID, info.AppName, info.UserID, metadataSessionID(info, filename), filename)
}

func makeObjectID(artifactID string, version int, sha string) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", fmt.Errorf("generate artifact object id: %w", err)
	}
	return "obj_" + scopedHash(
		artifactID,
		fmt.Sprintf("%d", version),
		sha,
		hex.EncodeToString(nonce[:]),
	), nil
}

func makeContentRef(artifactID string, version int) string {
	return fmt.Sprintf("artifact://%s?version=%d", artifactID, version)
}

func scopedHash(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		hash.Write([]byte(part))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:32]
}

func namespaceContainsSegment(namespace, tenantID string) bool {
	prefix := "tenant/" + tenantID
	return namespace == prefix || strings.HasPrefix(namespace, prefix+"/")
}

func hasLeadingOrTrailingSpace(value string) bool {
	return strings.TrimSpace(value) != value
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
