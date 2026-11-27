package sqlite

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/wins/jaz/backend/internal/storage"
	"github.com/wins/jaz/backend/internal/storage/sqlite/generated/settingsdb"
)

func (s *Store) ApplySettingChange(change storage.SettingChange, namespace, key string, value json.RawMessage) error {
	namespace, key, err := normalizeSettingRef(namespace, key)
	if err != nil {
		return err
	}
	value = append(json.RawMessage(nil), value...)
	if len(value) == 0 || !json.Valid(value) {
		return fmt.Errorf("setting %s/%s value must be valid JSON", namespace, key)
	}
	now := time.Now().UTC()
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := settingsdb.New(tx)
	if err := q.UpsertSetting(context.Background(), settingsdb.UpsertSettingParams{
		Namespace:   namespace,
		Key:         key,
		ValueJson:   string(value),
		CreatedAtMs: timeToMs(now),
		UpdatedAtMs: timeToMs(now),
	}); err != nil {
		return err
	}
	if err := q.InsertSettingChange(context.Background(), settingChangeParams(change)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordSettingChange(change storage.SettingChange) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return settingsdb.New(s.db).InsertSettingChange(context.Background(), settingChangeParams(change))
}

func (s *Store) ListSettingChanges(threadID string) ([]storage.SettingChange, error) {
	rows, err := settingsdb.New(s.db).ListSettingChanges(context.Background(), threadID)
	if err != nil {
		return nil, err
	}
	out := make([]storage.SettingChange, 0, len(rows))
	for _, row := range rows {
		out = append(out, storage.SettingChange{
			ThreadID: row.ThreadID, Actor: row.Actor, SettingKey: row.SettingKey, Scope: row.Scope, Risk: row.Risk,
			OldValue: row.OldValue, NewValue: row.NewValue, ApprovalRequired: row.ApprovalRequired != 0,
			ApprovalResult: row.ApprovalResult, Error: row.Error, At: msToTime(row.CreatedAtMs),
		})
	}
	return out, nil
}

func settingChangeParams(change storage.SettingChange) settingsdb.InsertSettingChangeParams {
	at := change.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	approved := int64(0)
	if change.ApprovalRequired {
		approved = 1
	}
	return settingsdb.InsertSettingChangeParams{
		ThreadID: change.ThreadID, Actor: change.Actor, SettingKey: change.SettingKey, Scope: change.Scope, Risk: change.Risk,
		OldValue: change.OldValue, NewValue: change.NewValue, ApprovalRequired: approved,
		ApprovalResult: change.ApprovalResult, Error: change.Error, CreatedAtMs: timeToMs(at),
	}
}
