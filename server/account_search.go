package server

import (
	"context"
	"errors"
	"time"

	actool "github.com/Autumn-27/norma/tool"
)

func (s *Server) configureAccountSearch(ctx context.Context, provider, key string) error {
	backend := provider
	if provider == "brave" {
		backend = "brave-free"
	}
	_, _, brave, tavily, proxy := s.m.WebSearch()
	if provider == "brave" {
		brave = key
	} else {
		tavily = key
	}
	probe, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	results, err := actool.WebSearchProbe(probe, actool.WebSearchConfig{Backend: backend, BraveAPIKey: brave, TavilyAPIKey: tavily, Proxy: proxy}, "Telegram official documentation", 2)
	if err != nil || len(results) == 0 {
		return errors.New("Search verification failed. Check this provider's API key and quota; the current configuration was preserved.")
	}
	var bk, tk *string
	if provider == "brave" {
		bk = &key
	} else {
		tk = &key
	}
	if err := s.m.SetWebSearch(true, backend, bk, tk, nil); err != nil {
		return errors.New("Could not save search configuration")
	}
	s.invalidateProfileAgents()
	s.reapplyActiveProfile()
	return nil
}
