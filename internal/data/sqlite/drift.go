package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/uuid"
	"github.com/pbufio/pbuf-registry/internal/data"
	"github.com/pbufio/pbuf-registry/internal/model"
)

type driftRepo struct {
	repo
}

// NewDriftRepository creates a new SQLite-backed DriftRepository.
func NewDriftRepository(db *sql.DB, logger log.Logger) data.DriftRepository {
	return &driftRepo{
		repo: repo{
			db:     db,
			logger: log.NewHelper(log.With(logger, "module", "data/sqlite/DriftRepository")),
		},
	}
}

func (d *driftRepo) GetTagsWithoutHashes(ctx context.Context) ([]string, error) {
	var tagIDs []string

	rows, err := d.db.QueryContext(ctx, `
		SELECT DISTINCT p.tag_id
		FROM protofiles p
		WHERE p.content_hash = ''
	`)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tagIDs, nil
		}
		d.logger.Errorf("error getting tags without hashes: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var tagID string
		if err := rows.Scan(&tagID); err != nil {
			d.logger.Errorf("error scanning tag id: %v", err)
			return nil, err
		}
		tagIDs = append(tagIDs, tagID)
	}
	if err := rows.Err(); err != nil {
		d.logger.Errorf("error iterating tags without hashes: %v", err)
		return nil, err
	}

	return tagIDs, nil
}

func (d *driftRepo) ComputeAndStoreHashes(ctx context.Context, tagID string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		d.logger.Errorf("error starting transaction: %v", err)
		return err
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			d.logger.Errorf("error rolling back transaction: %v", err)
		}
	}()

	rows, err := tx.QueryContext(ctx, `
		SELECT id, content
		FROM protofiles
		WHERE tag_id = ? AND content_hash = ''
	`, tagID)
	if err != nil {
		d.logger.Errorf("error getting protofiles for tag %s: %v", tagID, err)
		return err
	}
	defer rows.Close()

	type fileToHash struct {
		id      string
		content string
	}
	var filesToHash []fileToHash

	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			d.logger.Errorf("error scanning protofile: %v", err)
			return err
		}
		filesToHash = append(filesToHash, fileToHash{id: id, content: content})
	}
	if err := rows.Err(); err != nil {
		d.logger.Errorf("error iterating protofiles: %v", err)
		return err
	}

	for _, f := range filesToHash {
		hash := computeHash(f.content)
		_, err = tx.ExecContext(ctx, `
			UPDATE protofiles
			SET content_hash = ?
			WHERE id = ?
		`, hash, f.id)
		if err != nil {
			d.logger.Errorf("error updating content hash: %v", err)
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		d.logger.Errorf("error committing transaction: %v", err)
		return err
	}

	return nil
}

func (d *driftRepo) GetTagInfo(ctx context.Context, tagID string) (moduleID string, tagName string, err error) {
	err = d.db.QueryRowContext(ctx, `
		SELECT t.module_id, t.tag
		FROM tags t
		WHERE t.id = ?
	`, tagID).Scan(&moduleID, &tagName)
	if err != nil {
		d.logger.Errorf("error getting tag info: %v", err)
		return "", "", err
	}
	return moduleID, tagName, nil
}

func (d *driftRepo) GetPreviousTagID(ctx context.Context, moduleID string, currentTagID string) (string, error) {
	var currentUpdatedAtStr string
	err := d.db.QueryRowContext(ctx, `
		SELECT updated_at FROM tags WHERE id = ?
	`, currentTagID).Scan(&currentUpdatedAtStr)
	if err != nil {
		d.logger.Errorf("error getting current tag updated_at: %v", err)
		return "", err
	}

	currentUpdatedAt := parseTime(currentUpdatedAtStr)

	var previousTagID string
	err = d.db.QueryRowContext(ctx, `
		SELECT id
		FROM tags
		WHERE module_id = ? AND id != ? AND updated_at < ?
		ORDER BY updated_at DESC
		LIMIT 1
	`, moduleID, currentTagID, fmtTime(currentUpdatedAt)).Scan(&previousTagID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		d.logger.Errorf("error getting previous tag: %v", err)
		return "", err
	}
	return previousTagID, nil
}

func (d *driftRepo) GetFileHashesForTag(ctx context.Context, tagID string) (map[string]string, error) {
	files := make(map[string]string)
	rows, err := d.db.QueryContext(ctx, `
		SELECT filename, content_hash
		FROM protofiles
		WHERE tag_id = ? AND content_hash != ''
	`, tagID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return files, nil
		}
		d.logger.Errorf("error getting file hashes: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var filename, hash string
		if err := rows.Scan(&filename, &hash); err != nil {
			d.logger.Errorf("error scanning file hash: %v", err)
			return nil, err
		}
		files[filename] = hash
	}
	if err := rows.Err(); err != nil {
		d.logger.Errorf("error iterating file hashes: %v", err)
		return nil, err
	}
	return files, nil
}

func (d *driftRepo) GetProtoFileContents(ctx context.Context, tagID string) (map[string]string, error) {
	files := make(map[string]string)
	rows, err := d.db.QueryContext(ctx, `
		SELECT filename, content
		FROM protofiles
		WHERE tag_id = ?
	`, tagID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return files, nil
		}
		d.logger.Errorf("error getting proto file contents: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var filename, content string
		if err := rows.Scan(&filename, &content); err != nil {
			d.logger.Errorf("error scanning proto file content: %v", err)
			return nil, err
		}
		files[filename] = content
	}
	if err := rows.Err(); err != nil {
		d.logger.Errorf("error iterating proto file contents: %v", err)
		return nil, err
	}
	return files, nil
}

func (d *driftRepo) SaveDriftEvents(ctx context.Context, events []model.DriftEvent) error {
	if len(events) == 0 {
		return nil
	}

	const columnsPerRow = 9
	args := make([]interface{}, 0, len(events)*columnsPerRow)
	valueStrings := make([]string, 0, len(events))

	for _, event := range events {
		valueStrings = append(valueStrings, "(?,?,?,?,?,?,?,?,?)")

		prevHash := event.PreviousHash
		currHash := event.CurrentHash

		args = append(args,
			uuid.New().String(),
			event.ModuleID,
			event.TagID,
			event.Filename,
			event.EventType,
			prevHash,
			currHash,
			event.Severity,
			fmtTime(event.DetectedAt),
		)
	}

	query := fmt.Sprintf(`
		INSERT INTO drift_events (id, module_id, tag_id, filename, event_type, previous_hash, current_hash, severity, detected_at)
		VALUES %s
		ON CONFLICT (tag_id, filename, event_type, previous_hash, current_hash) DO NOTHING
	`, strings.Join(valueStrings, ", "))

	_, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		d.logger.Errorf("error batch inserting drift events: %v", err)
		return err
	}

	return nil
}

func (d *driftRepo) GetUnacknowledgedDriftEvents(ctx context.Context) ([]model.DriftEvent, error) {
	var events []model.DriftEvent

	rows, err := d.db.QueryContext(ctx, `
		SELECT de.id, de.module_id, m.name, de.tag_id, t.tag, de.filename, de.event_type, de.previous_hash, de.current_hash, de.severity, de.detected_at
		FROM drift_events de
		JOIN modules m ON de.module_id = m.id
		JOIN tags t ON de.tag_id = t.id
		WHERE de.acknowledged = 0
		ORDER BY de.detected_at DESC
	`)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return events, nil
		}
		d.logger.Errorf("error getting unacknowledged drift events: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var event model.DriftEvent
		var previousHash, currentHash string
		var detectedAtStr string
		if err := rows.Scan(
			&event.ID, &event.ModuleID, &event.ModuleName,
			&event.TagID, &event.TagName, &event.Filename,
			&event.EventType, &previousHash, &currentHash,
			&event.Severity, &detectedAtStr,
		); err != nil {
			d.logger.Errorf("error scanning drift event: %v", err)
			return nil, err
		}
		event.PreviousHash = previousHash
		event.CurrentHash = currentHash
		event.DetectedAt = parseTime(detectedAtStr)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		d.logger.Errorf("error iterating unacknowledged drift events: %v", err)
		return nil, err
	}

	return events, nil
}

func (d *driftRepo) AcknowledgeDriftEvent(ctx context.Context, eventID string, acknowledgedBy string) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE drift_events
		SET acknowledged = 1, acknowledged_at = datetime('now'), acknowledged_by = ?
		WHERE id = ?
	`, acknowledgedBy, eventID)
	if err != nil {
		d.logger.Errorf("error acknowledging drift event: %v", err)
		return err
	}
	return nil
}

func (d *driftRepo) GetDriftEventsForModule(ctx context.Context, moduleName string, tagName string) ([]model.DriftEvent, error) {
	var events []model.DriftEvent

	query := `
		SELECT de.id, de.module_id, m.name, de.tag_id, t.tag, de.filename, de.event_type, de.previous_hash, de.current_hash, de.severity, de.detected_at, de.acknowledged, de.acknowledged_at, de.acknowledged_by
		FROM drift_events de
		JOIN modules m ON de.module_id = m.id
		JOIN tags t ON de.tag_id = t.id
		WHERE m.name = ?`

	args := []interface{}{moduleName}

	if tagName != "" {
		query += ` AND t.tag = ?`
		args = append(args, tagName)
	}

	query += ` ORDER BY de.detected_at DESC`

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return events, nil
		}
		d.logger.Errorf("error getting drift events for module: %v", err)
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var event model.DriftEvent
		var previousHash, currentHash string
		var detectedAtStr string
		var acknowledgedInt int
		var acknowledgedAtStr *string
		var acknowledgedBy *string

		if err := rows.Scan(
			&event.ID, &event.ModuleID, &event.ModuleName,
			&event.TagID, &event.TagName, &event.Filename,
			&event.EventType, &previousHash, &currentHash,
			&event.Severity, &detectedAtStr, &acknowledgedInt,
			&acknowledgedAtStr, &acknowledgedBy,
		); err != nil {
			d.logger.Errorf("error scanning drift event: %v", err)
			return nil, err
		}

		event.PreviousHash = previousHash
		event.CurrentHash = currentHash
		event.DetectedAt = parseTime(detectedAtStr)
		event.Acknowledged = acknowledgedInt != 0

		if acknowledgedAtStr != nil {
			t := parseTime(*acknowledgedAtStr)
			event.AcknowledgedAt = &t
		}
		if acknowledgedBy != nil {
			event.AcknowledgedBy = *acknowledgedBy
		}

		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		d.logger.Errorf("error iterating drift events for module: %v", err)
		return nil, err
	}

	return events, nil
}

func (d *driftRepo) GetModuleDependencyDriftStatuses(ctx context.Context, moduleName string, tagName string) ([]model.DependencyDriftStatus, error) {
	var moduleTagID string
	resolvedTagName := tagName

	if tagName == "" {
		err := d.db.QueryRowContext(ctx, `
			SELECT t.id, t.tag
			FROM tags t
			JOIN modules m ON t.module_id = m.id
			WHERE m.name = ?
			ORDER BY t.updated_at DESC
			LIMIT 1
		`, moduleName).Scan(&moduleTagID, &resolvedTagName)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return []model.DependencyDriftStatus{}, nil
			}
			d.logger.Errorf("error getting latest tag for module %s: %v", moduleName, err)
			return nil, err
		}
	} else {
		err := d.db.QueryRowContext(ctx, `
			SELECT t.id
			FROM tags t
			JOIN modules m ON t.module_id = m.id
			WHERE m.name = ? AND t.tag = ?
		`, moduleName, tagName).Scan(&moduleTagID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, data.ErrTagNotFound
			}
			d.logger.Errorf("error getting tag %s for module %s: %v", tagName, moduleName, err)
			return nil, err
		}
	}

	type dependencyRef struct {
		moduleID string
		name     string
		tagID    string
		tagName  string
	}

	dependencies := make([]dependencyRef, 0)
	depRows, err := d.db.QueryContext(ctx, `
		SELECT dm.id, dm.name, dt.id, dt.tag
		FROM dependencies dep
		JOIN tags dt ON dep.dependency_tag_id = dt.id
		JOIN modules dm ON dt.module_id = dm.id
		WHERE dep.tag_id = ?
	`, moduleTagID)
	if err != nil {
		d.logger.Errorf("error getting dependencies for module %s tag %s: %v", moduleName, resolvedTagName, err)
		return nil, err
	}
	defer depRows.Close()

	for depRows.Next() {
		var dep dependencyRef
		if err := depRows.Scan(&dep.moduleID, &dep.name, &dep.tagID, &dep.tagName); err != nil {
			d.logger.Errorf("error scanning dependency for module %s tag %s: %v", moduleName, resolvedTagName, err)
			return nil, err
		}
		dependencies = append(dependencies, dep)
	}
	if err := depRows.Err(); err != nil {
		d.logger.Errorf("error iterating dependencies for module %s tag %s: %v", moduleName, resolvedTagName, err)
		return nil, err
	}

	statuses := make([]model.DependencyDriftStatus, 0)

	for _, dep := range dependencies {
		newerTagRows, err := d.db.QueryContext(ctx, `
			SELECT t.id, t.tag
			FROM tags t
			WHERE t.module_id = ?
				AND t.updated_at > (SELECT updated_at FROM tags WHERE id = ?)
			ORDER BY t.updated_at ASC
		`, dep.moduleID, dep.tagID)
		if err != nil {
			d.logger.Errorf("error getting newer tags for dependency %s@%s: %v", dep.name, dep.tagName, err)
			return nil, err
		}

		for newerTagRows.Next() {
			var newerTagID string
			var newerTagName string
			if err := newerTagRows.Scan(&newerTagID, &newerTagName); err != nil {
				newerTagRows.Close()
				d.logger.Errorf("error scanning newer dependency tag for %s: %v", dep.name, err)
				return nil, err
			}

			var maxSeverityRank int
			err = d.db.QueryRowContext(ctx, `
				SELECT COALESCE(MAX(
					CASE severity
						WHEN 'critical' THEN 3
						WHEN 'warning' THEN 2
						WHEN 'info' THEN 1
						ELSE 0
					END
				), 0)
				FROM drift_events
				WHERE module_id = ? AND tag_id = ?
			`, dep.moduleID, newerTagID).Scan(&maxSeverityRank)
			if err != nil {
				newerTagRows.Close()
				d.logger.Errorf("error getting drift severity for dependency %s target tag %s: %v", dep.name, newerTagName, err)
				return nil, err
			}

			if maxSeverityRank == 0 {
				continue
			}

			severity := severityRankToDriftSeverity(maxSeverityRank)
			statuses = append(statuses, model.DependencyDriftStatus{
				DependencyName: dep.name,
				CurrentTag:     dep.tagName,
				TargetTag:      newerTagName,
				Severity:       severity,
				Recommendation: recommendationFromSeverity(severity),
			})
		}

		if err := newerTagRows.Err(); err != nil {
			newerTagRows.Close()
			d.logger.Errorf("error iterating newer tags for dependency %s@%s: %v", dep.name, dep.tagName, err)
			return nil, err
		}
		newerTagRows.Close()
	}

	return statuses, nil
}

// computeHash computes SHA256 hash of the content.
func computeHash(content string) string {
	hash := sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:])
}

func severityRankToDriftSeverity(rank int) model.DriftSeverity {
	switch rank {
	case 3:
		return model.DriftSeverityCritical
	case 2:
		return model.DriftSeverityWarning
	default:
		return model.DriftSeverityInfo
	}
}

func recommendationFromSeverity(severity model.DriftSeverity) model.DependencyDriftRecommendation {
	if severity == model.DriftSeverityCritical || severity == model.DriftSeverityWarning {
		return model.DependencyDriftRecommendationAlertReview
	}
	return model.DependencyDriftRecommendationSuggestUpdate
}

