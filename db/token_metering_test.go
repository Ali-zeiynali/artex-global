package db

import "testing"

func TestExplorationTotalsPreferCallLedgerWithoutDoubleCounting(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	exp, err := d.CreateExploration("metering regression", "usage only")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, exp)
	defer d.Exec(`DELETE FROM llm_usage WHERE exploration_id=$1`, exp)
	s := d.Exploration(exp)
	in, out, read := 10, 7, 3
	if _, err := s.AppendActivity(Activity{Kind: "result", InputTokens: &in, OutputTokens: &out, CacheReadTokens: &read}); err != nil {
		t.Fatal(err)
	}
	legacy, err := s.TokenTotal()
	if err != nil {
		t.Fatal(err)
	}
	assertTokenUsage(t, legacy, 10, 7, 3, 0)
	beforeDaily, err := d.TokenDailyAll(30)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{"ok", "error"} {
		if err := d.InsertLLMUsage(&LLMUsage{ExplorationID: exp, InputTokens: 100, OutputTokens: 5, CacheRead: 80, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	total, err := s.TokenTotal()
	if err != nil {
		t.Fatal(err)
	}
	assertTokenUsage(t, total, 200, 10, 160, 0)
	all, err := d.TokenTotalsAll()
	if err != nil {
		t.Fatal(err)
	}
	assertTokenUsage(t, all[exp], 200, 10, 160, 0)
	afterDaily, err := d.TokenDailyAll(30)
	if err != nil {
		t.Fatal(err)
	}
	var dailyDelta TokenUsage
	for _, day := range beforeDaily {
		dailyDelta.InputTokens -= day.InputTokens
		dailyDelta.OutputTokens -= day.OutputTokens
		dailyDelta.CacheReadTokens -= day.CacheReadTokens
	}
	for _, day := range afterDaily {
		dailyDelta.InputTokens += day.InputTokens
		dailyDelta.OutputTokens += day.OutputTokens
		dailyDelta.CacheReadTokens += day.CacheReadTokens
	}
	assertTokenUsage(t, dailyDelta, 190, 3, 157, 0)
}
