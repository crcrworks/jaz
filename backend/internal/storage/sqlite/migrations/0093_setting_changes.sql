-- +goose Up
CREATE TABLE setting_changes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  thread_id TEXT NOT NULL,
  actor TEXT NOT NULL DEFAULT '',
  setting_key TEXT NOT NULL,
  scope TEXT NOT NULL,
  risk TEXT NOT NULL,
  old_value TEXT NOT NULL DEFAULT '',
  new_value TEXT NOT NULL DEFAULT '',
  approval_required INTEGER NOT NULL DEFAULT 0,
  approval_result TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at_ms INTEGER NOT NULL
);

CREATE INDEX setting_changes_thread ON setting_changes (thread_id, id);

-- +goose Down
DROP TABLE setting_changes;
