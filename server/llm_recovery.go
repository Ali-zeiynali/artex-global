package server

import "github.com/Autumn-27/artex/agent"

// A connection test uses a fresh client, so a successful test must also release
// any stale breaker on the saved connection it actually tested. Unsaved changes
// to a key, endpoint or model must not clear an unrelated saved profile's state.
func sameLLMConnection(a, b agent.Config) bool {
	return a.Format == b.Format && a.BaseURL == b.BaseURL && a.APIKey == b.APIKey &&
		a.Model == b.Model && a.Proxy == b.Proxy && a.Stream == b.Stream &&
		a.ThinkingType == b.ThinkingType && a.ReasoningEffort == b.ReasoningEffort &&
		a.SessionHeaderKey == b.SessionHeaderKey
}

func (s *Server) recoverTestedProfiles(tested agent.Config) {
	if s.m == nil || s.m.pg == nil || s.llmHealth == nil {
		return
	}
	profiles, err := s.m.pg.ListProfiles()
	if err != nil {
		return
	}
	recovered := false
	for _, profile := range profiles {
		stored, ok := s.loadProfileConfig(profile.ID)
		if ok && sameLLMConnection(stored, tested) {
			s.llmHealth.Reset(profile.ID)
			recovered = true
		}
	}
	if recovered {
		for _, task := range s.m.List() {
			task.Notify()
		}
	}
}
