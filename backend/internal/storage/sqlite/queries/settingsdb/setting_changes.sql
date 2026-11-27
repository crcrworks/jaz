-- name: InsertSettingChange :exec
INSERT INTO setting_changes (
  thread_id,
  actor,
  setting_key,
  scope,
  risk,
  old_value,
  new_value,
  approval_required,
  approval_result,
  error,
  created_at_ms
) VALUES (
  sqlc.arg(thread_id),
  sqlc.arg(actor),
  sqlc.arg(setting_key),
  sqlc.arg(scope),
  sqlc.arg(risk),
  sqlc.arg(old_value),
  sqlc.arg(new_value),
  sqlc.arg(approval_required),
  sqlc.arg(approval_result),
  sqlc.arg(error),
  sqlc.arg(created_at_ms)
);

-- name: ListSettingChanges :many
SELECT * FROM setting_changes
WHERE thread_id = sqlc.arg(thread_id)
ORDER BY id;
