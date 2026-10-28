package llmpool

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var zaiBurstLimitCode = regexp.MustCompile(`"code"\s*:\s*"?1302"?[,}]`)
var burstRecoveryDelay = func(round int) time.Duration { return time.Duration(2<<round) * time.Second }

// A spent backup must not turn a momentary 1302 on a usable primary into a
// permanent intent failure. Only re-probe members with that exact rate code,
// only after all fallbacks failed, and never after anything was delivered.
func burstRecoveryMembers(order []*Member, failures map[int64]error) []*Member {
	seen := map[int64]bool{}
	var out []*Member
	for _, m := range order {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		if err := failures[m.ID]; err != nil && statusOf(err) == 429 && zaiBurstLimitCode.MatchString(err.Error()) {
			out = append(out, m)
		}
	}
	return out
}

func waitBurstRecovery(ctx context.Context, round int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(burstRecoveryDelay(round))
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Preserve every member's cause: the last backup's quota error alone can hide
// a primary's temporary rate limit and mislead both the operator and retry code.
func exhaustedError(order []*Member, failures map[int64]error, fallback error) error {
	seen := map[int64]bool{}
	var causes []string
	for _, m := range order {
		if seen[m.ID] {
			continue
		}
		seen[m.ID] = true
		if err := failures[m.ID]; err != nil {
			causes = append(causes, fmt.Sprintf("%s (#%d): %v", m.Name, m.ID, err))
		}
	}
	if len(causes) == 0 {
		return fmt.Errorf("%w: %v", ErrExhausted, fallback)
	}
	return fmt.Errorf("%w: %s", ErrExhausted, strings.Join(causes, "; "))
}
