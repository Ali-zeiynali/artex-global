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
}
