package server

import (
	"testing"

	"github.com/Autumn-27/artex/agent"
)

func TestRecoveryRequiresTheSameTestedConnection(t *testing.T) {
	saved := agent.ConfigFrom("anthropic", "model", "https://api.example/v1/messages", "saved-key", "")
	if !sameLLMConnection(saved, saved) {
		t.Fatal("unchanged saved connection must be eligible for recovery")
	}
	for name, change := range map[string]func(*agent.Config){
		"key":      func(c *agent.Config) { c.APIKey = "different-key" },
		"endpoint": func(c *agent.Config) { c.BaseURL = "https://other.example" },
		"model":    func(c *agent.Config) { c.Model = "other-model" },
		"proxy":    func(c *agent.Config) { c.Proxy = "http://other.example:8080" },
		"stream":   func(c *agent.Config) { c.Stream = !c.Stream },
		"thinking": func(c *agent.Config) { c.ThinkingType = "enabled" },
		"effort":   func(c *agent.Config) { c.ReasoningEffort = "high" },
		"session":  func(c *agent.Config) { c.SessionHeaderKey = "X-Session" },
	} {
		t.Run(name, func(t *testing.T) {
			tested := saved
			change(&tested)
			if sameLLMConnection(saved, tested) {
				t.Fatal("successful test of an edited connection must not reset a different saved connection")
			}
		})
	}
}
