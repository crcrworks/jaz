package storage

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrSettingNotFound = errors.New("setting not found")

// Setting change outcomes an audit row records besides the approved and
// denied ones.
const (
	SettingApprovalApproved = "approved"
	SettingApprovalDenied   = "denied"
)

type Setting struct {
	Namespace string          `json:"namespace"`
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type SettingsStorage interface {
	LoadSetting(namespace, key string) (Setting, error)
	SaveSetting(namespace, key string, value json.RawMessage) (Setting, error)
	DeleteSetting(namespace, key string) error
	ListSettings(namespace string) ([]Setting, error)
}

// SettingChange is one recorded attempt to change a setting: who asked, what
// it held before, whether it needed the user's approval, and how it ended.
// Values are already redacted for the audit trail.
type SettingChange struct {
	ThreadID         string
	Actor            string
	SettingKey       string
	Scope            string
	Risk             string
	OldValue         string
	NewValue         string
	ApprovalRequired bool
	ApprovalResult   string
	Error            string
	At               time.Time
}

type SettingChangeStore interface {
	// ApplySettingChange stores the setting and its audit row atomically.
	ApplySettingChange(change SettingChange, namespace, key string, value json.RawMessage) error
	RecordSettingChange(change SettingChange) error
	ListSettingChanges(threadID string) ([]SettingChange, error)
}
