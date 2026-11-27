package sqlite

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/wins/jaz/backend/internal/storage"
)

func TestApplySettingChangeWritesTheSettingAndItsAuditRowTogether(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	change := storage.SettingChange{
		ThreadID: "bot-1", Actor: "Gimli", SettingKey: "memory.settings", Scope: "app-global", Risk: "standard",
		OldValue: `{"enabled":true}`, NewValue: `{"enabled":false}`, ApprovalRequired: true, ApprovalResult: storage.SettingApprovalApproved,
	}
	if err := store.ApplySettingChange(change, "memory", "settings", json.RawMessage(`{"enabled":false}`)); err != nil {
		t.Fatal(err)
	}
	setting, err := store.LoadSetting("memory", "settings")
	if err != nil || string(setting.Value) != `{"enabled":false}` {
		t.Fatalf("setting = %+v, %v", setting, err)
	}
	rows, err := store.ListSettingChanges("bot-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || !rows[0].ApprovalRequired || rows[0].ApprovalResult != storage.SettingApprovalApproved || rows[0].NewValue != change.NewValue {
		t.Fatalf("audit = %+v", rows)
	}

	if _, err := store.db.Exec(`CREATE TRIGGER reject_setting_change BEFORE INSERT ON setting_changes BEGIN SELECT RAISE(ABORT, 'audit unavailable'); END`); err != nil {
		t.Fatal(err)
	}
	err = store.ApplySettingChange(change, "memory", "settings", json.RawMessage(`{"enabled":true}`))
	if err == nil {
		t.Fatal("a failed audit row still stored the setting")
	}
	if setting, err = store.LoadSetting("memory", "settings"); err != nil || string(setting.Value) != `{"enabled":false}` {
		t.Fatalf("the failed change kept its setting: %+v, %v", setting, err)
	}
}

func TestRecordSettingChangeKeepsFailures(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.RecordSettingChange(storage.SettingChange{
		ThreadID: "bot-1", Actor: "Gimli", SettingKey: "bot.prompt", Scope: "bot-local", Risk: "standard",
		Error: "only the user can change this setting", At: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := store.ListSettingChanges("bot-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Error == "" || rows[0].ApprovalRequired {
		t.Fatalf("audit = %+v", rows)
	}
	if other, err := store.ListSettingChanges("bot-2"); err != nil || len(other) != 0 {
		t.Fatalf("another bot's audit = %+v, %v", other, err)
	}
}
