package server

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/norma/llm"
)

func TestShutdownRequeuesWorkerAndFreshEngineContinues(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("test postgres unavailable: %v", err)
	}
	defer m.Close()
	task, err := m.CreateTask("shutdown recovery regression", "finish after restart", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = m.DeleteTask(task.ID, DeleteTaskOptions{}) }()
	iid, err := task.Store.AddIntent(map[string]any{"summary": "finish after restart"}, 1, nil, "planner")
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	var calls atomic.Int32
	worker := agent.NewWorker(retestProvider{complete: func(ctx context.Context, _ llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			return llm.Message{}, "", llm.Usage{}, ctx.Err()
		}
		return llm.Message{Role: llm.RoleAssistant, Content: []llm.ContentBlock{llm.TextBlock("resumed successfully")}}, "end_turn", llm.Usage{}, nil
	}}, "test", m.dir, nil, 10000, 1)
	worker.SetNonStreaming(func() bool { return true })
	ctx, stop := context.WithCancelCause(context.Background())
	defer stop(nil)
	e := NewEngine(m)
	done := make(chan bool, 1)
	go func() { done <- e.runWorkerStep(ctx, task, "work#1", worker) }()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not reach provider")
	}
	stop(agent.AbortShutdown)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not settle worker")
	}
	node, err := task.Store.GetNode(iid)
	if err != nil || node.State != "open" {
		t.Fatalf("shutdown state=%+v err=%v; want open", node, err)
	}
	// A new engine must be able to claim the same intent without manual rerun.
	fresh := NewEngine(m)
	if !fresh.runWorkerStep(context.Background(), task, "work#1", worker) {
		t.Fatal("fresh engine did not reclaim intent")
	}
	node, err = task.Store.GetNode(iid)
	if err != nil || node.State != "done" {
		t.Fatalf("resumed state=%+v err=%v; want done", node, err)
	}
}
