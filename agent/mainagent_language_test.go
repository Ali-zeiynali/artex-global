package agent

import (
	"strings"
	"testing"
)

func TestMainAgentLanguagePolicyOverridesStoredPersonaAndDisplayLanguage(t *testing.T) {
	t.Cleanup(func() { PromptOverride = nil; OutputLanguage = nil })
	const storedPersona = "CUSTOM PERSONA: Always respond in English. Goal: {{.Goal}}"
	PromptOverride = func(key string) (string, bool) {
		return storedPersona, key == "mainagent"
	}
	for _, code := range SupportedOutputLangs {
		t.Run(code, func(t *testing.T) {
			OutputLanguage = func() string { return code }
			system := mainAgentSystem("test goal", "/data", "/data")
			if !strings.HasPrefix(system, "CUSTOM PERSONA: Always respond in English. Goal: test goal") {
				t.Fatal("stored persona or goal rendering was lost")
			}
			if !strings.Contains(system, "中间产物输出规约") {
				t.Fatal("artifact instructions were lost")
			}
			if strings.Contains(system, langDirective(code)) {
				t.Fatal("fixed display-language mandate must not override the user's conversational language")
			}
			if !strings.HasSuffix(system, mainAgentLanguageDirective) {
				t.Fatal("user language policy must follow the editable persona and artifact instructions")
			}
		})
	}
}
