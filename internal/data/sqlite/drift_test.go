package sqlite

import (
	"context"
	"testing"

	v1 "github.com/pbufio/pbuf-registry/gen/pbuf-registry/v1"
	"github.com/pbufio/pbuf-registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDriftModule registers a module, pushes a tag, and returns the tag ID.
func setupDriftModule(t *testing.T, name, tag string, files []*v1.ProtoFile) string {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, suite.registry.RegisterModule(ctx, name))
	_, err := suite.registry.PushModule(ctx, name, tag, files)
	require.NoError(t, err)
	tagID, err := suite.registry.GetModuleTagId(ctx, name, tag)
	require.NoError(t, err)
	require.NotEmpty(t, tagID)
	return tagID
}

func TestSQLite_ComputeAndStoreHashes(t *testing.T) {
	ctx := context.Background()
	files := []*v1.ProtoFile{
		{Filename: "svc.proto", Content: `syntax = "proto3"; package drift;`},
	}
	tagID := setupDriftModule(t, "test/drift-hashes", "v1.0.0", files)

	// before hashing, tag should appear in GetTagsWithoutHashes
	tags, err := suite.drift.GetTagsWithoutHashes(ctx)
	require.NoError(t, err)
	assert.Contains(t, tags, tagID)

	require.NoError(t, suite.drift.ComputeAndStoreHashes(ctx, tagID))

	// after hashing, tag should no longer appear
	tags, err = suite.drift.GetTagsWithoutHashes(ctx)
	require.NoError(t, err)
	assert.NotContains(t, tags, tagID)

	hashes, err := suite.drift.GetFileHashesForTag(ctx, tagID)
	require.NoError(t, err)
	assert.NotEmpty(t, hashes["svc.proto"])
}

func TestSQLite_SaveAndGetDriftEvents(t *testing.T) {
	ctx := context.Background()
	files := []*v1.ProtoFile{{Filename: "e.proto", Content: "syntax=\"proto3\";"}}
	tagID := setupDriftModule(t, "test/drift-events", "v1.0.0", files)

	module, err := suite.registry.GetModule(ctx, "test/drift-events")
	require.NoError(t, err)

	events := []model.DriftEvent{
		{
			ModuleID:     module.Id,
			ModuleName:   "test/drift-events",
			TagID:        tagID,
			TagName:      "v1.0.0",
			Filename:     "e.proto",
			EventType:    model.DriftEventTypeModified,
			PreviousHash: "aabbcc",
			CurrentHash:  "ddeeff",
			Severity:     model.DriftSeverityCritical,
		},
	}
	require.NoError(t, suite.drift.SaveDriftEvents(ctx, events))

	unacked, err := suite.drift.GetUnacknowledgedDriftEvents(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, unacked)

	var found *model.DriftEvent
	for i := range unacked {
		if unacked[i].ModuleName == "test/drift-events" {
			found = &unacked[i]
			break
		}
	}
	require.NotNil(t, found)
	assert.Equal(t, model.DriftEventTypeModified, found.EventType)
	assert.Equal(t, model.DriftSeverityCritical, found.Severity)

	// acknowledge it
	require.NoError(t, suite.drift.AcknowledgeDriftEvent(ctx, found.ID, "tester"))

	modEvents, err := suite.drift.GetDriftEventsForModule(ctx, "test/drift-events", "v1.0.0")
	require.NoError(t, err)
	require.NotEmpty(t, modEvents)
	assert.True(t, modEvents[0].Acknowledged)
}
