package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"iter"
	"sync"

	"github.com/Autumn-27/norma/llm"
)

// Some Anthropic-compatible endpoints (notably Z.ai) report input/cache only
// in message_delta. Norma 0.4.3 discards those fields. Observe only usage frames
// at the transport boundary, then emit incremental counts to all consumers,
// including the harness and recorder. Never retain prompt or response content.
type anthropicUsageProvider struct{ inner llm.Provider }
type anthropicUsageKey struct{}
type anthropicUsageFrame struct {
	kind  string
	usage llm.Usage
}
type anthropicUsageObserver struct {
	mu       sync.Mutex
	line     []byte
	dropping bool
	frames   []anthropicUsageFrame
}

func (o *anthropicUsageObserver) read(p []byte) {
	o.mu.Lock()
	defer o.mu.Unlock()
	for _, b := range p {
		if b == '\n' {
			if !o.dropping {
				o.parse(o.line)
			}
			o.line = o.line[:0]
			o.dropping = false
		} else if !o.dropping {
			if len(o.line) >= 64*1024 {
				o.line = o.line[:0]
				o.dropping = true
			} else {
				o.line = append(o.line, b)
			}
		}
	}
}

type anthropicWireUsage struct {
	Input  int `json:"input_tokens"`
	Output int `json:"output_tokens"`
	Read   int `json:"cache_read_input_tokens"`
	Write  int `json:"cache_creation_input_tokens"`
}

func (o *anthropicUsageObserver) parse(line []byte) {
	if !bytes.HasPrefix(line, []byte("data:")) {
		return
	}
	var f struct {
		Type    string              `json:"type"`
		Usage   *anthropicWireUsage `json:"usage"`
		Message struct {
			Usage *anthropicWireUsage `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(bytes.TrimSpace(line[5:]), &f) != nil {
		return
	}
	u := f.Usage
	if f.Type == "message_start" {
		u = f.Message.Usage
	}
	if u == nil || (f.Type != "message_start" && f.Type != "message_delta") {
		return
	}
	v := llm.Usage{InputTokens: max(0, u.Input) + max(0, u.Read) + max(0, u.Write), OutputTokens: max(0, u.Output), CacheReadTokens: max(0, u.Read), CacheWriteTokens: max(0, u.Write)}
	if f.Type == "message_start" {
		v.OutputTokens = 0
	}
	o.frames = append(o.frames, anthropicUsageFrame{kind: f.Type, usage: v})
}

func (o *anthropicUsageObserver) take(kind string) (llm.Usage, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.frames) == 0 || o.frames[0].kind != kind {
		return llm.Usage{}, false
	}
	f := o.frames[0]
	o.frames = o.frames[1:]
	return f.usage, true
}

type anthropicUsageBody struct {
	io.ReadCloser
	observer *anthropicUsageObserver
}

func (b *anthropicUsageBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.observer.read(p[:n])
	}
	return n, err
}

func (p *anthropicUsageProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		o := &anthropicUsageObserver{}
		ctx = context.WithValue(ctx, anthropicUsageKey{}, o)
		var reported llm.Usage
		for ev, err := range p.inner.Stream(ctx, req) {
			kind := ""
			if ev.Type == llm.SEMessageStart {
				kind = "message_start"
			}
			if ev.Type == llm.SEMessageDelta {
				kind = "message_delta"
			}
			if kind != "" && err == nil {
				if cumulative, ok := o.take(kind); ok {
					ev.Usage = llm.Usage{InputTokens: max(0, cumulative.InputTokens-reported.InputTokens), OutputTokens: max(0, cumulative.OutputTokens-reported.OutputTokens), CacheReadTokens: max(0, cumulative.CacheReadTokens-reported.CacheReadTokens), CacheWriteTokens: max(0, cumulative.CacheWriteTokens-reported.CacheWriteTokens)}
				}
				reported.Add(ev.Usage)
			}
			if !yield(ev, err) {
				return
			}
		}
	}
}

func (p *anthropicUsageProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return p.inner.Complete(ctx, req)
}
