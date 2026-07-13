//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package artifactstore

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	_ MetadataStore = (*InMemoryMetadataStore)(nil)
	_ ObjectStore   = (*InMemoryObjectStore)(nil)
)

// InMemoryMetadataStore stores metadata records in memory for tests and local runs.
type InMemoryMetadataStore struct {
	mu      sync.RWMutex
	records []MetadataRecord
}

// NewInMemoryMetadataStore creates an empty in-memory metadata store.
func NewInMemoryMetadataStore() *InMemoryMetadataStore {
	return &InMemoryMetadataStore{}
}

// Put inserts or replaces one metadata record.
func (s *InMemoryMetadataStore) Put(ctx context.Context, record MetadataRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if record.Status == "" {
		record.Status = MetadataStatusActive
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.records {
		if sameVersion(existing, record) {
			return ErrVersionConflict
		}
	}
	s.records = append(s.records, record)
	return nil
}

// Reserve atomically allocates the next version and stores its pending metadata.
func (s *InMemoryMetadataStore) Reserve(
	ctx context.Context,
	query MetadataQuery,
	build MetadataReservationBuilder,
) (MetadataRecord, error) {
	if err := ctx.Err(); err != nil {
		return MetadataRecord{}, err
	}
	if build == nil {
		return MetadataRecord{}, errors.New("artifactstore reservation builder is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records := make([]MetadataRecord, 0)
	for _, record := range s.records {
		if matchMetadata(record, query) {
			records = append(records, record)
		}
	}
	record, err := build(nextVersion(records))
	if err != nil {
		return MetadataRecord{}, err
	}
	if record.Status == "" {
		record.Status = MetadataStatusPending
	}
	for _, existing := range s.records {
		if sameVersion(existing, record) {
			return MetadataRecord{}, ErrVersionConflict
		}
	}
	s.records = append(s.records, record)
	return record, nil
}

// Query returns records matching all non-empty query fields.
func (s *InMemoryMetadataStore) Query(ctx context.Context, query MetadataQuery) ([]MetadataRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]MetadataRecord, 0)
	for _, record := range s.records {
		if !query.IncludePending && record.Status == MetadataStatusPending {
			continue
		}
		if !query.IncludeDeleting && record.Status == MetadataStatusDeleting {
			continue
		}
		if !matchMetadata(record, query) {
			continue
		}
		records = append(records, record)
	}
	sortMetadata(records)
	return records, nil
}

// Activate publishes one pending metadata reservation.
func (s *InMemoryMetadataStore) Activate(
	ctx context.Context,
	query MetadataQuery,
	objectID string,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for index := range s.records {
		record := &s.records[index]
		if !matchMetadata(*record, query) || record.ObjectID != objectID {
			continue
		}
		if record.Status == MetadataStatusActive {
			return nil
		}
		if record.Status != MetadataStatusPending {
			return ErrMetadataReservationNotFound
		}
		record.Status = MetadataStatusActive
		return nil
	}
	return ErrMetadataReservationNotFound
}

// MarkDeleting atomically hides matching records and returns cleanup tombstones.
func (s *InMemoryMetadataStore) MarkDeleting(
	ctx context.Context,
	query MetadataQuery,
) ([]MetadataRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	indexes := make([]int, 0)
	for index := range s.records {
		if !matchMetadata(s.records[index], query) {
			continue
		}
		if s.records[index].Status == MetadataStatusPending && !query.AllowPendingTransition {
			expiresAt := s.records[index].ReservationExpiresAt
			if expiresAt.IsZero() || time.Now().Before(expiresAt) {
				return nil, ErrArtifactWriteInProgress
			}
		}
		indexes = append(indexes, index)
	}
	records := make([]MetadataRecord, 0, len(indexes))
	for _, index := range indexes {
		s.records[index].Status = MetadataStatusDeleting
		records = append(records, s.records[index])
	}
	sortMetadata(records)
	return records, nil
}

// Delete removes records matching all non-empty query fields and returns them.
func (s *InMemoryMetadataStore) Delete(ctx context.Context, query MetadataQuery) ([]MetadataRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	deleted := make([]MetadataRecord, 0)
	kept := s.records[:0]
	for _, record := range s.records {
		if matchMetadata(record, query) {
			deleted = append(deleted, record)
			continue
		}
		kept = append(kept, record)
	}
	s.records = kept
	sortMetadata(deleted)
	return deleted, nil
}

// HasInlineContent reports whether metadata contains embedded object bytes.
func (s *InMemoryMetadataStore) HasInlineContent(artifactID string) bool {
	return false
}

// InMemoryObjectStore stores object bytes in memory for tests and local runs.
type InMemoryObjectStore struct {
	mu          sync.RWMutex
	objects     map[string]objectValue
	failNextPut []error
	putAttempts int
}

type objectValue struct {
	data []byte
	key  string
}

// NewInMemoryObjectStore creates an empty in-memory object store.
func NewInMemoryObjectStore() *InMemoryObjectStore {
	return &InMemoryObjectStore{
		objects: make(map[string]objectValue),
	}
}

// Put stores object bytes by opaque object ID.
func (s *InMemoryObjectStore) Put(ctx context.Context, object ObjectRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putAttempts++
	if len(s.failNextPut) > 0 {
		err := s.failNextPut[0]
		s.failNextPut = s.failNextPut[1:]
		return err
	}
	if existing, ok := s.objects[object.ObjectID]; ok {
		if bytes.Equal(existing.data, object.Data) {
			return nil
		}
		return ErrObjectConflict
	}
	s.objects[object.ObjectID] = objectValue{
		data: append([]byte(nil), object.Data...),
		key:  "objects/" + object.TenantID + "/" + object.ObjectID,
	}
	return nil
}

// Get returns a copy of object bytes.
func (s *InMemoryObjectStore) Get(ctx context.Context, objectID string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	object, ok := s.objects[objectID]
	if !ok {
		return nil, ErrObjectNotFound
	}
	return append([]byte(nil), object.data...), nil
}

// Delete removes object bytes.
func (s *InMemoryObjectStore) Delete(ctx context.Context, objectID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.objects, objectID)
	return nil
}

// FailNextPut makes future Put calls fail with the supplied error in FIFO order.
func (s *InMemoryObjectStore) FailNextPut(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failNextPut = append(s.failNextPut, err)
}

// PutAttempts returns the number of Put attempts made.
func (s *InMemoryObjectStore) PutAttempts() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.putAttempts
}

// RawKey returns the internal object key for tests that check content-ref leakage.
func (s *InMemoryObjectStore) RawKey(objectID string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if object, ok := s.objects[objectID]; ok {
		return object.key
	}
	return "objects/" + objectID
}

// ObjectIDs returns all currently stored object IDs.
func (s *InMemoryObjectStore) ObjectIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.objects))
	for id := range s.objects {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

// MustData returns object data or fails the test.
func (s *InMemoryObjectStore) MustData(t testingT, objectID string) []byte {
	t.Helper()
	data, err := s.Get(context.Background(), objectID)
	if err != nil {
		t.Fatalf("object %q not found: %v", objectID, err)
	}
	return data
}

func sameVersion(left MetadataRecord, right MetadataRecord) bool {
	return left.TenantID == right.TenantID &&
		left.AppName == right.AppName &&
		left.UserID == right.UserID &&
		left.SessionID == right.SessionID &&
		left.Filename == right.Filename &&
		left.Version == right.Version
}

func matchMetadata(record MetadataRecord, query MetadataQuery) bool {
	if query.TenantID != "" && record.TenantID != query.TenantID {
		return false
	}
	if query.AppName != "" && record.AppName != query.AppName {
		return false
	}
	if query.UserID != "" && record.UserID != query.UserID {
		return false
	}
	if query.SessionID != "" && record.SessionID != query.SessionID {
		return false
	}
	if query.Filename != "" && record.Filename != query.Filename {
		return false
	}
	if query.ObjectID != "" && record.ObjectID != query.ObjectID {
		return false
	}
	if query.ReservationOwner != "" && record.ReservationOwner != query.ReservationOwner {
		return false
	}
	if query.Version != nil && record.Version != *query.Version {
		return false
	}
	return true
}
