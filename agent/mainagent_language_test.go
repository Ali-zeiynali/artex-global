package agent

import (
	"strings"
	"testing"
)

func TestMainAgentLanguagePolicyOverridesStoredPersonaAndDisplayLanguage(t *testing.T) {
	t.Cleanup(func() { PromptOverride = nil; OutputLanguage = nil })
	const storedPersona = "CUSTOM PERSONA: Always respond in English. Data: {{.DataDir}}"
	PromptOverride = func(key string) (string, bool) {
		return storedPersona, key == "mainagent"
	}
	for _, code := range SupportedOutputLangs {
		t.Run(code, func(t *testing.T) {
			OutputLanguage = func() string { return code }
			for mode, system := range map[string]string{
				"task":       mainAgentSystem("test goal", "/data", "/data"),
				"standalone": chatSystem("mainagent", "/data", "/data"),
			} {
				if !strings.HasPrefix(system, "CUSTOM PERSONA: Always respond in English. Data: /data") {
					t.Fatalf("%s: stored persona or variable rendering was lost", mode)
				}
				if mode == "task" && !strings.Contains(system, "中间产物输出规约") {
					t.Fatal("artifact instructions were lost")
				}
				if strings.Contains(system, langDirective(code)) {
					t.Fatalf("%s: fixed display-language mandate must not override the user's conversational language", mode)
				}
				if !strings.HasSuffix(system, mainAgentLanguageDirective) {
					t.Fatalf("%s: user language policy must follow the editable persona and artifact instructions", mode)
				}
			}
		})
	}
}
