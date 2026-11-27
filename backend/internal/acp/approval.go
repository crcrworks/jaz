package acp

import (
	"context"
	"fmt"
	"strings"

	"github.com/wins/jaz/backend/internal/sessionevents"
)

const (
	ApprovalOptionApprove = "approve"
	ApprovalOptionDeny    = "deny"
)

// RequestApproval asks the user, in the surface showing sessionID, to approve
// an action a tool cannot take on its own. It returns true only when the user
// approves; a denial, an interrupt or a missing session all leave false.
func (m *Manager) RequestApproval(ctx context.Context, sessionID, title, body string, locations []sessionevents.ACPPermissionLocation) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	job := m.jobByID(sessionID)
	if job == nil {
		return false, fmt.Errorf("approval request needs an active Jaz thread: %s", sessionID)
	}
	permission := sessionevents.ACPPermission{
		ID:        newPermissionID(),
		Title:     strings.TrimSpace(title),
		Content:   strings.TrimSpace(body),
		Locations: locations,
		Status:    "pending",
		Options: []sessionevents.ACPPermissionOption{
			{ID: ApprovalOptionApprove, Name: "Approve", Kind: "approve"},
			{ID: ApprovalOptionDeny, Name: "Deny", Kind: "deny"},
		},
	}
	answer := m.awaitPermissionAnswer(ctx, job, permission, nil)
	return answer.OptionID == ApprovalOptionApprove, nil
}
