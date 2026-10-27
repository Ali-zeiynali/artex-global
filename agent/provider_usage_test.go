package agent

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Autumn-27/norma/llm"
)

func TestAnthropicStreamingUsageFinalCounters(t *testing.T) {
	for _, tc := range []struct {
		name, start, delta string
		want               llm.Usage
	}{
		{"late input and cache", `{"input_tokens":0,"output_tokens":0}`, `{"input_tokens":119,"output_tokens":541,"cache_read_input_tokens":327104}`, llm.Usage{InputTokens: 327223, OutputTokens: 541, CacheReadTokens: 327104}},
		{"echoed input counted once", `{"input_tokens":11,"cache_read_input_tokens":31,"cache_creation_input_tokens":7,"output_tokens":1}`, `{"input_tokens":11,"cache_read_input_tokens":31,"cache_creation_input_tokens":7,"output_tokens":9}`, llm.Usage{InputTokens: 49, OutputTokens: 9, CacheReadTokens: 31, CacheWriteTokens: 7}},
		{"standard output only delta", `{"input_tokens":11,"cache_read_input_tokens":31,"output_tokens":1}`, `{"output_tokens":9}`, llm.Usage{InputTokens: 42, OutputTokens: 9, CacheReadTokens: 31}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprintf(w, "data: {\"type\":\"message_start\",\"message\":{\"usage\":%s}}\n\n", tc.start)
				fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"OK\"}}\n\n")
				// Multiple cumulative delta frames must not double-count output either.
				for range 2 {
					fmt.Fprintf(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":%s}\n\n", tc.delta)
				}
				fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
			}))
			defer s.Close()
			p, err := (Config{Format: llm.FormatAnthropic, APIKey: "fixture", Model: "fixture", BaseURL: s.URL}).NewProvider()
			if err != nil {
				t.Fatal(err)
			}
			var got llm.Usage
			for ev, err := range p.Stream(t.Context(), llm.CompletionRequest{Messages: []llm.Message{llm.UserText("ping")}}) {
				if err != nil {
					t.Fatal(err)
				}
				if ev.Type == llm.SEMessageStart || ev.Type == llm.SEMessageDelta {
					got.Add(ev.Usage)
				}
			}
			if got != tc.want {
				t.Fatalf("usage=%+v want %+v", got, tc.want)
			}
		})
	}
}

func TestAnthropicUsageObserverFragmentationAndBound(t *testing.T) {
	o := &anthropicUsageObserver{}
	line := []byte("data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":3,\"cache_read_input_tokens\":17,\"output_tokens\":9}}\r\n")
	for _, b := range line {
		o.read([]byte{b})
	}
	u, ok := o.take("message_delta")
	if !ok || u.InputTokens != 20 || u.CacheReadTokens != 17 || u.OutputTokens != 9 {
		t.Fatal(u, ok)
	}
	for range 100000 {
		o.read([]byte{'x'})
	}
	if len(o.line) > 65536 || len(o.frames) != 0 {
		t.Fatal("unbounded non-usage data")
	}
	o.read([]byte{'\n'})
	o.read(line)
	if _, ok := o.take("message_delta"); !ok {
		t.Fatal("did not recover after oversized content line")
	}
}
