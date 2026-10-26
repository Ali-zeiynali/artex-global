package llmpool

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/norma/llm"
)

func TestRecoveredCoolingBackupIsNotStranded(t *testing.T) {
	for _, stream := range []bool{true, false} {
		for _, headCooling := range []bool{true, false} {
			reg := NewRegistry(nil, nil)
			reg.Restore(2, State{Trips: 3, OpenUntil: time.Now().Add(30 * time.Minute)})
			if headCooling {
				reg.Restore(1, State{Trips: 3, OpenUntil: time.Now().Add(30 * time.Minute)})
			}
			head, backup := failProv("head", 429), okProv("backup", "recovered")
			pool := New([]*Member{member(1, "head", RankActive, head), member(2, "backup", 10, backup)}, reg)
			var err error
			if stream {
				_, err = drain(pool.Stream(context.Background(), llm.CompletionRequest{}))
			} else {
				_, _, _, err = pool.Complete(context.Background(), llm.CompletionRequest{})
			}
			if err != nil || head.calls != 1 || backup.calls != 1 {
				t.Fatalf("stream=%v headCooling=%v: err=%v calls=%d/%d; recovered backup must be tried", stream, headCooling, err, head.calls, backup.calls)
			}
			if reg.IsOpen(2) {
				t.Fatal("successful backup must clear its stale breaker")
			}
			_, err = drain(pool.Stream(context.Background(), llm.CompletionRequest{}))
			if err != nil {
				t.Fatal(err)
			}
			if headCooling && head.calls != 1 {
				t.Fatal("healthy backup must prevent another probe of the cooling head")
			}
		}
	}
}

func TestCoolingMemberIsOnlyALastResort(t *testing.T) {
	head, backup := okProv("head", "ok"), failProv("backup", 429)
	reg := NewRegistry(nil, nil)
	reg.Restore(2, State{Trips: 3, OpenUntil: time.Now().Add(30 * time.Minute)})
	pool := New([]*Member{member(1, "head", RankActive, head), member(2, "backup", 10, backup)}, reg)
	if _, err := drain(pool.Stream(context.Background(), llm.CompletionRequest{})); err != nil || backup.calls != 0 {
		t.Fatalf("healthy head must not probe cooling backup: err=%v backup.calls=%d", err, backup.calls)
	}
}

func TestCoolingFallbackStillRespectsContextCapacity(t *testing.T) {
	head, small, backup := failProv("head", 429), okProv("small", "wrong"), okProv("backup", "ok")
	reg := NewRegistry(nil, nil)
	reg.Restore(2, State{Trips: 3, OpenUntil: time.Now().Add(time.Minute)})
	reg.Restore(3, State{Trips: 3, OpenUntil: time.Now().Add(time.Minute)})
	tooSmall := member(2, "small", 20, small)
	tooSmall.WindowTokens = 10
	pool := New([]*Member{member(1, "head", RankActive, head), tooSmall, member(3, "backup", 10, backup)}, reg)
	req := llm.CompletionRequest{Messages: []llm.Message{llm.UserText(strings.Repeat("x", 1000))}}
	if _, err := drain(pool.Stream(context.Background(), req)); err != nil || small.calls != 0 || backup.calls != 1 {
		t.Fatalf("err=%v small.calls=%d backup.calls=%d", err, small.calls, backup.calls)
	}
}
