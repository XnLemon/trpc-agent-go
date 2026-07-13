//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package storagerouter

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode"

	"trpc.group/trpc-go/trpc-agent-go/platform"
)

var (
	statusRedactorOnce sync.Once
	statusRedactor     *platform.Redactor
	statusRedactorErr  error
)

// ResourceStatus describes the routing readiness of one storage resource.
type ResourceStatus string

const (
	// ResourceStatusReady means the selected backend is registered and has the resource service.
	ResourceStatusReady ResourceStatus = "ready"
	// ResourceStatusBackendMissing means the profile points at an unregistered backend.
	ResourceStatusBackendMissing ResourceStatus = "backend_missing"
	// ResourceStatusServiceMissing means the backend is registered without the requested service.
	ResourceStatusServiceMissing ResourceStatus = "service_missing"
	// ResourceStatusBackendTenantMismatch means the selected backend belongs to another tenant.
	ResourceStatusBackendTenantMismatch ResourceStatus = "backend_tenant_mismatch"
)

// ResourceStatusEntry is a safe operations-facing status for one routed resource.
type ResourceStatusEntry struct {
	Resource  platform.BackendMigrationResource
	BackendID string
	Status    ResourceStatus
	Reason    string
}

// StatusSummary is a safe operations-facing view of one storage profile route.
type StatusSummary struct {
	TenantID      string
	ProfileID     string
	MigrationMode platform.StorageMigrationMode
	IsMigrating   bool
	Resources     []ResourceStatusEntry
	ReadyCount    int
	MissingCount  int
}

// Status summarizes the backend readiness for one registered storage profile.
func (r *InMemoryRouter) Status(
	ctx context.Context,
	tenantID string,
	profileID string,
) (StatusSummary, error) {
	profile, err := r.Profile(ctx, tenantID, profileID)
	if err != nil {
		return StatusSummary{}, err
	}
	mode, err := platform.NormalizeStorageMigrationMode(profile.MigrationMode)
	if err != nil {
		return StatusSummary{}, err
	}
	summary := StatusSummary{
		TenantID:      profile.TenantID,
		ProfileID:     profile.ProfileID,
		MigrationMode: mode,
		IsMigrating:   platform.IsActiveStorageMigrationMode(mode),
	}

	for _, resource := range []struct {
		kind     resourceKind
		resource platform.BackendMigrationResource
	}{
		{kind: resourceSession, resource: platform.BackendMigrationResourceSession},
		{kind: resourceSummary, resource: platform.BackendMigrationResourceSummary},
		{kind: resourceMemory, resource: platform.BackendMigrationResourceMemory},
		{kind: resourceArtifact, resource: platform.BackendMigrationResourceArtifact},
		{kind: resourceKnowledge, resource: platform.BackendMigrationResourceKnowledge},
		{kind: resourceAudit, resource: platform.BackendMigrationResourceAudit},
	} {
		if err := ctx.Err(); err != nil {
			return StatusSummary{}, err
		}
		entry := r.resourceStatus(ctx, profile, resource.kind, resource.resource)
		summary.Resources = append(summary.Resources, entry)
		if entry.Status == ResourceStatusReady {
			summary.ReadyCount++
		} else {
			summary.MissingCount++
		}
	}
	return summary, nil
}

func (r *InMemoryRouter) resourceStatus(
	ctx context.Context,
	profile platform.StorageProfile,
	kind resourceKind,
	resource platform.BackendMigrationResource,
) ResourceStatusEntry {
	entry := ResourceStatusEntry{
		Resource: resource,
	}
	backendID := backendIDFor(profile, kind)
	entry.BackendID = safeBackendIDForStatus(backendID)
	if strings.TrimSpace(backendID) == "" {
		entry.Status = ResourceStatusBackendMissing
		entry.Reason = fmt.Sprintf("%s backend is not configured", resource)
		return entry
	}
	if entry.BackendID == "" {
		entry.Status = ResourceStatusBackendMissing
		entry.Reason = fmt.Sprintf("%s backend id is unsafe to expose", resource)
		return entry
	}

	r.mu.RLock()
	backend, ok := r.backends[backendKey{tenantID: profile.TenantID, backendID: backendID}]
	r.mu.RUnlock()
	if !ok {
		entry.Status = ResourceStatusBackendMissing
		entry.Reason = fmt.Sprintf("%s backend is not registered", resource)
		return entry
	}
	// Defensive for future persistent backends that may not key by tenant_id.
	if backend.TenantID != profile.TenantID {
		entry.Status = ResourceStatusBackendTenantMismatch
		entry.Reason = fmt.Sprintf("%s backend belongs to another tenant", resource)
		return entry
	}
	if !backendHasResource(backend, kind) {
		entry.Status = ResourceStatusServiceMissing
		entry.Reason = fmt.Sprintf("%s service is not registered on selected backend", resource)
		return entry
	}
	entry.Status = ResourceStatusReady
	return entry
}

func backendHasResource(backend BackendSet, kind resourceKind) bool {
	switch kind {
	case resourceSession:
		return backend.Session != nil
	case resourceSummary:
		return backend.Summary != nil
	case resourceMemory:
		return backend.Memory != nil
	case resourceArtifact:
		return backend.Artifact != nil
	case resourceKnowledge:
		return backend.Knowledge != nil
	case resourceAudit:
		return backend.Audit != nil
	default:
		return false
	}
}

func safeBackendIDForStatus(backendID string) string {
	backendID = strings.TrimSpace(backendID)
	if backendID == "" || strings.Contains(backendID, "://") ||
		strings.ContainsAny(backendID, "=@/\\") {
		return ""
	}
	redactor, err := statusBackendIDRedactor()
	if err != nil || redactor.Redact(backendID) != backendID {
		return ""
	}
	for _, r := range backendID {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			continue
		}
		switch r {
		case '-', '_', '.':
			continue
		default:
			return ""
		}
	}
	return backendID
}

func statusBackendIDRedactor() (*platform.Redactor, error) {
	statusRedactorOnce.Do(func() {
		statusRedactor, statusRedactorErr = platform.NewRedactor()
	})
	return statusRedactor, statusRedactorErr
}
