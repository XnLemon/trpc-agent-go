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
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"trpc.group/trpc-go/trpc-agent-go/artifact"
)

func TestServiceStoresMetadataSeparatelyFromObjectContent(t *testing.T) {
	ctx := context.Background()
	metadata := NewInMemoryMetadataStore()
	objects := NewInMemoryObjectStore()
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}

	version, err := service.SaveArtifact(ctx, sessionInfo, "diagram.png", &artifact.Artifact{
		Data:     []byte("png-bytes"),
		MimeType: "image/png",
		Name:     "diagram.png",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, version)

	record, err := service.Metadata(ctx, sessionInfo, "diagram.png", &version)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, "tenant-a", record.TenantID)
	assert.Equal(t, "tenant/tenant-a/app-a", record.AppName)
	assert.Equal(t, "internal-user-a", record.UserID)
	assert.Equal(t, "session-a", record.SessionID)
	assert.Equal(t, "diagram.png", record.Filename)
	assert.Equal(t, "image/png", record.MimeType)
	assert.Equal(t, int64(len("png-bytes")), record.SizeBytes)
	assert.NotEmpty(t, record.SHA256)
	assert.Equal(t, "image", record.AttachmentKind)
	for _, secret := range []string{
		"secret",
		record.TenantID,
		record.AppName,
		record.UserID,
		record.SessionID,
		record.Filename,
		record.ObjectID,
		objects.RawKey(record.ObjectID),
	} {
		assert.NotContains(t, record.ContentRef, secret)
	}
	assert.False(t, metadata.HasInlineContent(record.ArtifactID))
	assert.Equal(t, []byte("png-bytes"), objects.MustData(t, record.ObjectID))

	loaded, err := service.LoadArtifact(ctx, sessionInfo, "diagram.png", nil)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, []byte("png-bytes"), loaded.Data)
	assert.Equal(t, "image/png", loaded.MimeType)
	assert.Equal(t, "diagram.png", loaded.Name)
	assert.Empty(t, loaded.URL)

	records, err := metadata.Query(ctx, MetadataQuery{
		TenantID:  "tenant-a",
		AppName:   "tenant/tenant-a/app-a",
		SessionID: "session-a",
	})
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, record.ArtifactID, records[0].ArtifactID)
}

func TestServiceRejectsSessionIDReservedForUserArtifacts(t *testing.T) {
	ctx := context.Background()
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: NewInMemoryMetadataStore(),
		ObjectStore:   NewInMemoryObjectStore(),
	})
	require.NoError(t, err)

	_, err = service.SaveArtifact(ctx, artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: userArtifactSessionID,
	}, "notes.txt", &artifact.Artifact{Data: []byte("notes")})

	require.ErrorIs(t, err, ErrEmptySessionInfo)
}

func TestServiceUserArtifactsDoNotShareSessionBucket(t *testing.T) {
	ctx := context.Background()
	metadata := NewInMemoryMetadataStore()
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   NewInMemoryObjectStore(),
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}

	_, err = service.SaveArtifact(ctx, sessionInfo, "user:profile.txt", &artifact.Artifact{
		Data:     []byte("profile"),
		MimeType: "text/plain",
		Name:     "user:profile.txt",
	})
	require.NoError(t, err)
	record, err := service.Metadata(ctx, sessionInfo, "user:profile.txt", nil)
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, userArtifactSessionID, record.SessionID)

	sessionRecords, err := metadata.Query(ctx, MetadataQuery{
		TenantID:  "tenant-a",
		AppName:   sessionInfo.AppName,
		UserID:    sessionInfo.UserID,
		SessionID: sessionInfo.SessionID,
		Filename:  "user:profile.txt",
	})
	require.NoError(t, err)
	assert.Empty(t, sessionRecords)
}

func TestServiceRejectsCrossTenantAndCrossUserAccess(t *testing.T) {
	ctx := context.Background()
	metadata := NewInMemoryMetadataStore()
	objects := NewInMemoryObjectStore()
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}
	_, err = service.SaveArtifact(ctx, sessionInfo, "report.pdf", &artifact.Artifact{
		Data:     []byte("pdf"),
		MimeType: "application/pdf",
		Name:     "report.pdf",
	})
	require.NoError(t, err)

	_, err = service.LoadArtifact(ctx, artifact.SessionInfo{
		AppName:   "tenant/tenant-b/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}, "report.pdf", nil)
	require.ErrorIs(t, err, ErrOutsideTenantScope)

	loaded, err := service.LoadArtifact(ctx, artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-b",
		SessionID: "session-a",
	}, "report.pdf", nil)
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestServiceRetriesTransientObjectUploadFailure(t *testing.T) {
	ctx := context.Background()
	metadata := NewInMemoryMetadataStore()
	objects := NewInMemoryObjectStore()
	objects.FailNextPut(errors.New("temporary object store failure"))
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
		MaxAttempts:   2,
	})
	require.NoError(t, err)

	version, err := service.SaveArtifact(ctx, artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}, "attachment.txt", &artifact.Artifact{
		Data:     []byte("hello"),
		MimeType: "text/plain",
		Name:     "attachment.txt",
	})
	require.NoError(t, err)
	assert.Equal(t, 0, version)
	assert.Equal(t, 2, objects.PutAttempts())
}

func TestServiceDeleteRemovesMetadataAndObjects(t *testing.T) {
	ctx := context.Background()
	metadata := NewInMemoryMetadataStore()
	objects := NewInMemoryObjectStore()
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}
	for _, content := range []string{"v0", "v1"} {
		_, err := service.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
			Data:     []byte(content),
			MimeType: "text/plain",
			Name:     "notes.txt",
		})
		require.NoError(t, err)
	}

	err = service.DeleteArtifact(ctx, sessionInfo, "notes.txt")
	require.NoError(t, err)

	versions, err := service.ListVersions(ctx, sessionInfo, "notes.txt")
	require.NoError(t, err)
	assert.Empty(t, versions)
	assert.Empty(t, objects.ObjectIDs())
}

func TestServiceDeleteKeepsObjectsWhenMarkDeletingFails(t *testing.T) {
	ctx := context.Background()
	metadata := &failingDeleteMetadataStore{InMemoryMetadataStore: NewInMemoryMetadataStore()}
	objects := NewInMemoryObjectStore()
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}
	version, err := service.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
		Data:     []byte("v0"),
		MimeType: "text/plain",
		Name:     "notes.txt",
	})
	require.NoError(t, err)
	record, err := service.Metadata(ctx, sessionInfo, "notes.txt", &version)
	require.NoError(t, err)
	require.NotNil(t, record)
	metadata.failDelete = errors.New("metadata delete unavailable")

	err = service.DeleteArtifact(ctx, sessionInfo, "notes.txt")
	require.Error(t, err)

	stillPresent, err := service.Metadata(ctx, sessionInfo, "notes.txt", &version)
	require.NoError(t, err)
	require.NotNil(t, stillPresent)
	assert.Equal(t, []byte("v0"), objects.MustData(t, record.ObjectID))
}

func TestServiceDeleteRetriesMetadataCleanupAfterObjectDelete(t *testing.T) {
	ctx := context.Background()
	metadata := &failingMetadataDeleteStore{
		InMemoryMetadataStore: NewInMemoryMetadataStore(),
		failDelete:            errors.New("metadata delete unavailable"),
	}
	objects := NewInMemoryObjectStore()
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}
	_, err = service.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
		Data:     []byte("v0"),
		MimeType: "text/plain",
		Name:     "notes.txt",
	})
	require.NoError(t, err)

	err = service.DeleteArtifact(ctx, sessionInfo, "notes.txt")
	require.Error(t, err)
	loaded, err := service.LoadArtifact(ctx, sessionInfo, "notes.txt", nil)
	require.NoError(t, err)
	assert.Nil(t, loaded)
	assert.Empty(t, objects.ObjectIDs())
	pending, err := metadata.Query(ctx, MetadataQuery{
		TenantID:        "tenant-a",
		AppName:         sessionInfo.AppName,
		UserID:          sessionInfo.UserID,
		SessionID:       sessionInfo.SessionID,
		Filename:        "notes.txt",
		IncludeDeleting: true,
	})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, MetadataStatusDeleting, pending[0].Status)

	require.NoError(t, service.DeleteArtifact(ctx, sessionInfo, "notes.txt"))
	pending, err = metadata.Query(ctx, MetadataQuery{
		TenantID:        "tenant-a",
		AppName:         sessionInfo.AppName,
		UserID:          sessionInfo.UserID,
		SessionID:       sessionInfo.SessionID,
		Filename:        "notes.txt",
		IncludeDeleting: true,
	})
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestServiceSupportsNestedArtifactFilenames(t *testing.T) {
	ctx := context.Background()
	metadata := NewInMemoryMetadataStore()
	objects := NewInMemoryObjectStore()
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}

	_, err = service.SaveArtifact(ctx, sessionInfo, "out/site.zip", &artifact.Artifact{
		Data:     []byte("zip"),
		MimeType: "application/zip",
		Name:     "site.zip",
	})
	require.NoError(t, err)

	loaded, err := service.LoadArtifact(ctx, sessionInfo, "out/site.zip", nil)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, []byte("zip"), loaded.Data)
	keys, err := service.ListArtifactKeys(ctx, sessionInfo)
	require.NoError(t, err)
	assert.Equal(t, []string{"out/site.zip"}, keys)
}

func TestServiceDeleteRetriesPendingObjectCleanup(t *testing.T) {
	ctx := context.Background()
	metadata := NewInMemoryMetadataStore()
	objects := &failingDeleteObjectStore{
		InMemoryObjectStore: NewInMemoryObjectStore(),
		failDelete:          errors.New("object delete unavailable"),
	}
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}
	version, err := service.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
		Data:     []byte("v0"),
		MimeType: "text/plain",
		Name:     "notes.txt",
	})
	require.NoError(t, err)
	record, err := service.Metadata(ctx, sessionInfo, "notes.txt", &version)
	require.NoError(t, err)
	require.NotNil(t, record)

	err = service.DeleteArtifact(ctx, sessionInfo, "notes.txt")
	require.Error(t, err)

	loaded, err := service.LoadArtifact(ctx, sessionInfo, "notes.txt", nil)
	require.NoError(t, err)
	assert.Nil(t, loaded)
	pending, err := metadata.Query(ctx, MetadataQuery{
		TenantID:        "tenant-a",
		AppName:         sessionInfo.AppName,
		UserID:          sessionInfo.UserID,
		SessionID:       sessionInfo.SessionID,
		Filename:        "notes.txt",
		IncludeDeleting: true,
	})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, MetadataStatusDeleting, pending[0].Status)
	assert.Equal(t, []byte("v0"), objects.MustData(t, record.ObjectID))

	err = service.DeleteArtifact(ctx, sessionInfo, "notes.txt")
	require.NoError(t, err)
	pending, err = metadata.Query(ctx, MetadataQuery{
		TenantID:        "tenant-a",
		AppName:         sessionInfo.AppName,
		UserID:          sessionInfo.UserID,
		SessionID:       sessionInfo.SessionID,
		Filename:        "notes.txt",
		IncludeDeleting: true,
	})
	require.NoError(t, err)
	assert.Empty(t, pending)
	assert.Empty(t, objects.ObjectIDs())
}

func TestServiceFailedMetadataActivationKeepsRetryableCleanupTombstone(t *testing.T) {
	ctx := context.Background()
	metadata := &failingActivateMetadataStore{
		InMemoryMetadataStore: NewInMemoryMetadataStore(),
		failActivate:          errors.New("metadata activation unavailable"),
	}
	objects := &failingDeleteObjectStore{
		InMemoryObjectStore: NewInMemoryObjectStore(),
		failDelete:          errors.New("object cleanup unavailable"),
	}
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}

	_, err = service.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
		Data:     []byte("v0"),
		MimeType: "text/plain",
		Name:     "notes.txt",
	})
	require.Error(t, err)

	loaded, err := service.LoadArtifact(ctx, sessionInfo, "notes.txt", nil)
	require.NoError(t, err)
	assert.Nil(t, loaded)
	require.Len(t, objects.ObjectIDs(), 1)
	pending, err := metadata.Query(ctx, MetadataQuery{
		TenantID:        "tenant-a",
		AppName:         sessionInfo.AppName,
		UserID:          sessionInfo.UserID,
		SessionID:       sessionInfo.SessionID,
		Filename:        "notes.txt",
		IncludeDeleting: true,
	})
	require.NoError(t, err)
	require.Len(t, pending, 1)
	assert.Equal(t, MetadataStatusDeleting, pending[0].Status)

	require.NoError(t, service.DeleteArtifact(ctx, sessionInfo, "notes.txt"))
	pending, err = metadata.Query(ctx, MetadataQuery{
		TenantID:        "tenant-a",
		AppName:         sessionInfo.AppName,
		UserID:          sessionInfo.UserID,
		SessionID:       sessionInfo.SessionID,
		Filename:        "notes.txt",
		IncludeDeleting: true,
	})
	require.NoError(t, err)
	assert.Empty(t, pending)
	assert.Empty(t, objects.ObjectIDs())
}

func TestServiceFailedActivationCleansUpAfterRequestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	metadata := &failingActivateMetadataStore{
		InMemoryMetadataStore: NewInMemoryMetadataStore(),
		failActivate:          errors.New("metadata activation unavailable"),
		cancel:                cancel,
	}
	objects := NewInMemoryObjectStore()
	service, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}

	_, err = service.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
		Data:     []byte("v0"),
		MimeType: "text/plain",
		Name:     "notes.txt",
	})
	require.Error(t, err)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	assert.Empty(t, objects.ObjectIDs())
	pending, err := metadata.Query(context.Background(), MetadataQuery{
		TenantID:        "tenant-a",
		AppName:         sessionInfo.AppName,
		UserID:          sessionInfo.UserID,
		SessionID:       sessionInfo.SessionID,
		Filename:        "notes.txt",
		IncludePending:  true,
		IncludeDeleting: true,
	})
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestServiceConcurrentDeleteAndReuploadKeepsNewObject(t *testing.T) {
	ctx := context.Background()
	metadata := NewInMemoryMetadataStore()
	objects := &blockingDeleteObjectStore{
		InMemoryObjectStore: NewInMemoryObjectStore(),
		deleteStarted:       make(chan struct{}),
		continueDelete:      make(chan struct{}),
	}
	deleteService, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	saveService, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}
	content := []byte("same-content")
	_, err = saveService.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
		Data:     content,
		MimeType: "text/plain",
		Name:     "notes.txt",
	})
	require.NoError(t, err)

	deleteDone := make(chan error, 1)
	go func() {
		deleteDone <- deleteService.DeleteArtifact(ctx, sessionInfo, "notes.txt")
	}()
	<-objects.deleteStarted

	version, err := saveService.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
		Data:     content,
		MimeType: "text/plain",
		Name:     "notes.txt",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, version)
	close(objects.continueDelete)
	require.NoError(t, <-deleteDone)

	loaded, err := saveService.LoadArtifact(ctx, sessionInfo, "notes.txt", nil)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, content, loaded.Data)
	versions, err := saveService.ListVersions(ctx, sessionInfo, "notes.txt")
	require.NoError(t, err)
	assert.Equal(t, []int{1}, versions)
}

func TestServiceDeleteDoesNotCancelPendingUpload(t *testing.T) {
	ctx := context.Background()
	metadata := NewInMemoryMetadataStore()
	objects := &blockingFirstPutObjectStore{
		InMemoryObjectStore: NewInMemoryObjectStore(),
		firstPutStarted:     make(chan struct{}),
		continueFirstPut:    make(chan struct{}),
	}
	firstService, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	secondService, err := New(ServiceConfig{
		TenantID:      "tenant-a",
		Namespace:     "tenant/tenant-a",
		MetadataStore: metadata,
		ObjectStore:   objects,
	})
	require.NoError(t, err)
	sessionInfo := artifact.SessionInfo{
		AppName:   "tenant/tenant-a/app-a",
		UserID:    "internal-user-a",
		SessionID: "session-a",
	}

	firstDone := make(chan struct {
		version int
		err     error
	}, 1)
	go func() {
		version, saveErr := firstService.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
			Data:     []byte("first"),
			MimeType: "text/plain",
			Name:     "notes.txt",
		})
		firstDone <- struct {
			version int
			err     error
		}{version: version, err: saveErr}
	}()
	<-objects.firstPutStarted

	err = secondService.DeleteArtifact(ctx, sessionInfo, "notes.txt")
	require.ErrorIs(t, err, ErrArtifactWriteInProgress)

	secondVersion, err := secondService.SaveArtifact(ctx, sessionInfo, "notes.txt", &artifact.Artifact{
		Data:     []byte("second"),
		MimeType: "text/plain",
		Name:     "notes.txt",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, secondVersion)

	close(objects.continueFirstPut)
	firstResult := <-firstDone
	require.NoError(t, firstResult.err)
	assert.Equal(t, 0, firstResult.version)

	loaded, err := secondService.LoadArtifact(ctx, sessionInfo, "notes.txt", nil)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, []byte("second"), loaded.Data)
	versions, err := secondService.ListVersions(ctx, sessionInfo, "notes.txt")
	require.NoError(t, err)
	assert.Equal(t, []int{0, 1}, versions)
	assert.Len(t, objects.ObjectIDs(), 2)
}

type failingDeleteMetadataStore struct {
	*InMemoryMetadataStore
	failDelete error
}

type failingMetadataDeleteStore struct {
	*InMemoryMetadataStore
	failDelete error
}

func (s *failingMetadataDeleteStore) Delete(
	ctx context.Context,
	query MetadataQuery,
) ([]MetadataRecord, error) {
	if s.failDelete != nil {
		err := s.failDelete
		s.failDelete = nil
		return nil, err
	}
	return s.InMemoryMetadataStore.Delete(ctx, query)
}

type failingActivateMetadataStore struct {
	*InMemoryMetadataStore
	failActivate error
	cancel       context.CancelFunc
}

func (s *failingActivateMetadataStore) Activate(
	ctx context.Context,
	query MetadataQuery,
	objectID string,
) error {
	if s.failActivate != nil {
		err := s.failActivate
		s.failActivate = nil
		if s.cancel != nil {
			s.cancel()
		}
		return err
	}
	return s.InMemoryMetadataStore.Activate(ctx, query, objectID)
}

type failingDeleteObjectStore struct {
	*InMemoryObjectStore
	failDelete error
}

func (s *failingDeleteObjectStore) Delete(ctx context.Context, objectID string) error {
	if s.failDelete != nil {
		err := s.failDelete
		s.failDelete = nil
		return err
	}
	return s.InMemoryObjectStore.Delete(ctx, objectID)
}

type blockingDeleteObjectStore struct {
	*InMemoryObjectStore
	deleteStarted  chan struct{}
	continueDelete chan struct{}
	once           sync.Once
}

func (s *blockingDeleteObjectStore) Delete(ctx context.Context, objectID string) error {
	s.once.Do(func() {
		close(s.deleteStarted)
		<-s.continueDelete
	})
	return s.InMemoryObjectStore.Delete(ctx, objectID)
}

type blockingFirstPutObjectStore struct {
	*InMemoryObjectStore
	mu               sync.Mutex
	putCalls         int
	firstPutStarted  chan struct{}
	continueFirstPut chan struct{}
}

func (s *blockingFirstPutObjectStore) Put(ctx context.Context, object ObjectRecord) error {
	s.mu.Lock()
	s.putCalls++
	call := s.putCalls
	s.mu.Unlock()
	if call == 1 {
		close(s.firstPutStarted)
		<-s.continueFirstPut
	}
	return s.InMemoryObjectStore.Put(ctx, object)
}

func (s *failingDeleteMetadataStore) MarkDeleting(
	ctx context.Context,
	query MetadataQuery,
) ([]MetadataRecord, error) {
	if s.failDelete != nil {
		err := s.failDelete
		s.failDelete = nil
		return nil, err
	}
	return s.InMemoryMetadataStore.MarkDeleting(ctx, query)
}
