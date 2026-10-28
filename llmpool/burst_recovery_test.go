package llmpool

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/norma/llm"
)

func noBurstWait(t *testing.T) {
	t.Helper()
	old := burstRecoveryDelay
	burstRecoveryDelay = func(int) time.Duration { return 0 }
	t.Cleanup(func() { burstRecoveryDelay = old })
}
func burstError() error {
	return fmt.Errorf(`anthropic: status 429: {"error":{"code":"1302","message":"Rate limit reached for requests"}}`)
}
func spentBackupError() error {
	return fmt.Errorf(`anthropic: status 402: {"error":{"code":"1308","message":"Usage limit reached for 5 hour"}}`)
}

func TestTransientPrimaryRecoversAfterSpentBackup(t *testing.T) {
	noBurstWait(t)
	for _, stream := range []bool{true, false} {
		primary := &fakeProv{errs: []error{burstError(), nil}, events: [][]llm.StreamEvent{nil, {{Type: llm.SETextDelta, Text: "recovered"}}}}
		backup := &fakeProv{errs: []error{spentBackupError()}}
		p := New([]*Member{member(1, "primary", 10, primary), member(2, "backup", 1, backup)}, NewRegistry(nil, nil))
		var err error
		if stream {
			_, err = drain(p.Stream(context.Background(), llm.CompletionRequest{}))
		} else {
			_, _, _, err = p.Complete(context.Background(), llm.CompletionRequest{})
		}
		if err != nil || primary.calls != 2 || backup.calls != 1 {
			t.Fatalf("stream=%v error=%v calls=%d/%d", stream, err, primary.calls, backup.calls)
		}
	}
}

func TestBurstRecoveryBoundedAndAllCausesVisible(t *testing.T) {
	noBurstWait(t)
	a := &fakeProv{errs: []error{burstError()}}
	b := &fakeProv{errs: []error{spentBackupError()}}
	p := New([]*Member{member(1, "primary", 10, a), member(2, "backup", 1, b)}, NewRegistry(nil, nil))
	_, err := drain(p.Stream(context.Background(), llm.CompletionRequest{}))
	if !errors.Is(err, ErrExhausted) || a.calls != 3 || b.calls != 1 || !strings.Contains(err.Error(), "1302") || !strings.Contains(err.Error(), "1308") {
		t.Fatal(err, a.calls, b.calls)
	}
}

func TestNoBurstRecoveryAfterOutputOrCancellation(t *testing.T) {
	noBurstWait(t)
	a := &fakeProv{errs: []error{burstError()}, events: [][]llm.StreamEvent{{{Type: llm.SETextDelta, Text: "partial"}}}}
	b := &fakeProv{errs: []error{spentBackupError()}}
	p := New([]*Member{member(1, "primary", 10, a), member(2, "backup", 1, b)}, NewRegistry(nil, nil))
	text, err := drain(p.Stream(context.Background(), llm.CompletionRequest{}))
	if err == nil || text != "partial" || a.calls != 1 || b.calls != 0 {
		t.Fatal("output was replayed", err, a.calls, b.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitBurstRecovery(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
