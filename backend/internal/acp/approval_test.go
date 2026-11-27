package acp

import (
	"context"
	"testing"
	"time"

	"github.com/wins/jaz/backend/internal/sessionevents"
	"github.com/wins/jaz/backend/internal/storage"
	jsonstore "github.com/wins/jaz/backend/internal/storage/json"
)

func approvalFixture(t *testing.T) (*Manager, storage.Session, context.Context) {
	t.Helper()
	store, err := jsonstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(storage.CreateSession{Slug: "approval", Runtime: storage.RuntimeACP})
	if err != nil {
		t.Fatal(err)
	}
	manager := NewManager(store, Config{}, nil)
	manager.Events = sessionevents.New()
	manager.jobsByID[session.ID] = &jobState{Job: Job{ID: session.ID, ACPSession: "native-session", State: StateRunning}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return manager, session, ctx
}

func TestRequestApprovalFollowsTheUsersAnswer(t *testing.T) {
	for _, option := range []string{ApprovalOptionApprove, ApprovalOptionDeny} {
		t.Run(option, func(t *testing.T) {
			manager, session, ctx := approvalFixture(t)
			events := manager.Events.Subscribe(ctx, session.ID)
			answer := make(chan bool, 1)
			go func() {
				allowed, _ := manager.RequestApproval(ctx, session.ID, "Change memory.settings", "before/after", nil)
				answer <- allowed
			}()
			var request sessionevents.ACPPermission
			for request.ID == "" {
				select {
				case event := <-events:
					if event.Type == "permission_request" {
						request = *event.Permission
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			if request.Title != "Change memory.settings" || len(request.Options) != 2 {
				t.Fatalf("approval card = %+v", request)
			}
			if err := manager.AnswerInteractive(ctx, InteractiveAnswer{Session: session.ID, RequestID: request.ID, OptionID: option}); err != nil {
				t.Fatal(err)
			}
			if allowed := <-answer; allowed != (option == ApprovalOptionApprove) {
				t.Fatalf("%s returned allowed = %v", option, allowed)
			}
		})
	}
}

func TestRequestApprovalWithoutAnActiveThreadFails(t *testing.T) {
	manager, session, ctx := approvalFixture(t)
	if _, err := manager.RequestApproval(ctx, session.ID+"-gone", "", "", nil); err == nil {
		t.Fatal("approval request with no active thread succeeded")
	}
}
