package subagent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pockyHM/conan/internal/llm"
	"github.com/pockyHM/conan/pkg/models"
)

func TestManagerSubmitReturnsChannelsAndID(t *testing.T) {
	mgr := NewManager()
	runner := Runner{
		Provider: &endlessToolCallProvider{},
		Executor: &fakeExecutor{},
	}

	id, events, results, err := mgr.Submit(context.Background(), runner, Request{
		Role:     RoleInvestigator,
		Task:     "x",
		MaxTurns: 10,
	})
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}
	if id == "" {
		t.Fatal("id is empty")
	}
	if events == nil || results == nil {
		t.Fatal("channels are nil")
	}
	_ = events
	_ = results
	mgr.CancelAll()
}

func TestManagerCancelStopsSubagent(t *testing.T) {
	mgr := NewManager()
	runner := Runner{Provider: &blockingProvider{}, Executor: &fakeExecutor{}}

	id, _, results, err := mgr.Submit(context.Background(), runner, Request{
		Role:     RoleInvestigator,
		Task:     "x",
		MaxTurns: 10,
	})
	if err != nil {
		t.Fatalf("Submit error: %v", err)
	}

	if err := mgr.Cancel(id); err != nil {
		t.Fatalf("Cancel error: %v", err)
	}

	select {
	case res := <-results:
		if !errors.Is(res.Err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", res.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("results channel did not deliver after Cancel")
	}
}

func TestManagerCancelUnknownIDIsNoop(t *testing.T) {
	mgr := NewManager()
	if err := mgr.Cancel("does-not-exist"); err != nil {
		t.Errorf("Cancel unknown id = %v, want nil", err)
	}
}

func TestManagerCancelAllStopsAll(t *testing.T) {
	mgr := NewManager()
	runner := Runner{Provider: &blockingProvider{}, Executor: &fakeExecutor{}}

	ids := make([]string, 0, 3)
	results := make([]<-chan Result, 0, 3)
	for i := 0; i < 3; i++ {
		id, _, r, err := mgr.Submit(context.Background(), runner, Request{
			Role:     RoleInvestigator,
			Task:     "x",
			MaxTurns: 10,
		})
		if err != nil {
			t.Fatalf("Submit error: %v", err)
		}
		ids = append(ids, id)
		results = append(results, r)
	}

	mgr.CancelAll()

	for i, r := range results {
		select {
		case res := <-r:
			if !errors.Is(res.Err, context.Canceled) {
				t.Errorf("results[%d] err = %v, want context.Canceled", i, res.Err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("results[%d] did not deliver after CancelAll", i)
		}
	}
}

func TestManagerResultChannelClosesAfterResult(t *testing.T) {
	mgr := NewManager()
	runner := Runner{Provider: &endlessToolCallProvider{}, Executor: &fakeExecutor{}}

	_, _, results, err := mgr.Submit(context.Background(), runner, Request{
		Role:     RoleInvestigator,
		Task:     "x",
		MaxTurns: 1,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}

	<-results
	select {
	case _, ok := <-results:
		if ok {
			t.Error("results channel still open after result received")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("results channel did not close")
	}
}

func TestManagerSubmitAutoIDMatchesResultID(t *testing.T) {
	mgr := NewManager()
	runner := Runner{Provider: &fakeProvider{}, Executor: &fakeExecutor{}}

	id, _, results, err := mgr.Submit(context.Background(), runner, Request{
		Role:     RoleInvestigator,
		Task:     "x",
		MaxTurns: 4,
		MaxToolCalls: 4,
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res := <-results
	if res.ID != id {
		t.Errorf("result.ID = %q, want %q (manager's auto-generated id)", res.ID, id)
	}
	mgr.CancelAll()
}

var _ = time.Second
var _ = models.NewID
var _ llm.ToolCall
