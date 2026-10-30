package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Autumn-27/artex/db"
)

type accountCredentialField struct {
	Key        string `json:"key"`
	Configured bool   `json:"configured"`
}
type accountConnection struct {
	ID            string                   `json:"id"`
	Kind          string                   `json:"kind"`
	Name          string                   `json:"name"`
	Enabled       bool                     `json:"enabled"`
	Configured    bool                     `json:"configured"`
	Managed       bool                     `json:"managed"`
	Endpoint      string                   `json:"endpoint,omitempty"`
	NeedsEndpoint bool                     `json:"needs_endpoint"`
	Fields        []accountCredentialField `json:"fields"`
	Note          string                   `json:"note,omitempty"`
}

func credentialPresent(value string) bool {
	v := strings.TrimSpace(value)
	return v != "" && v != "Bearer" && !strings.Contains(v, "your-") && !strings.Contains(v, "REPLACE_ME")
}
func credentialFieldName(key string) bool {
	k := strings.ToLower(key)
	return strings.Contains(k, "token") || strings.Contains(k, "api_key") || strings.Contains(k, "api-key") || strings.Contains(k, "password") || k == "authorization"
}

func mcpCredentialFields(m *db.MCPServer) []accountCredentialField {
	env := jsonStrMap(m.Env)
	var fields []accountCredentialField
	for key, value := range env {
		if credentialFieldName(key) {
			fields = append(fields, accountCredentialField{key, credentialPresent(value)})
		}
	}
	if len(fields) == 0 && strings.Contains(strings.ToLower(m.Name), "token-pending") {
		fields = append(fields, accountCredentialField{"Authorization", false})
	}
	if len(fields) == 0 && strings.EqualFold(m.Name, "context7") {
		fields = append(fields, accountCredentialField{"CONTEXT7_API_KEY", false})
	}
	return fields
}

func (s *Server) accountConnections(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var result []accountConnection
	servers, err := pg.ListMCP()
	if err != nil {
		writeErr(w, 500, "Could not load connections")
		return
	}
	for _, m := range servers {
		fields := mcpCredentialFields(m)
		if len(fields) == 0 {
			continue
		}
		managed := m.Name == "rea" || m.Name == "gmail" || m.Name == "metasploit-official" || strings.HasPrefix(m.Name, "gmail-") || strings.HasPrefix(m.Name, "telegram-") || strings.HasPrefix(m.Name, "atomicmail-")
		configured := true
		for _, f := range fields {
			configured = configured && f.Configured
		}
		endpoint := m.URL
		// A provider URL can contain a credential in its query. Never echo queries.
		if u, e := url.Parse(endpoint); e == nil {
			u.RawQuery = ""
			u.User = nil
			endpoint = u.String()
		}
		note := "Token is stored on the server and is never returned here."
		if managed {
			note = "Managed by the connected account or internal service. Use its account card to reconnect."
		}
		result = append(result, accountConnection{ID: strconv.FormatInt(m.ID, 10), Kind: "mcp", Name: strings.TrimSuffix(m.Name, "-token-pending"), Enabled: m.Enabled, Configured: configured, Managed: managed, Endpoint: endpoint, NeedsEndpoint: m.Transport != "stdio" && m.URL == "", Fields: fields, Note: note})
	}
	profiles, err := pg.ListProfiles()
	if err != nil {
		writeErr(w, 500, "Could not load model credentials")
		return
	}
	for _, p := range profiles {
		full, e := pg.ProfileByID(p.ID)
		if e != nil || full == nil {
			continue
		}
		result = append(result, accountConnection{ID: strconv.FormatInt(p.ID, 10), Kind: "llm", Name: p.Name, Enabled: p.IsDefault, Configured: full.APIKey != "", Fields: []accountCredentialField{{"api_key", full.APIKey != ""}}, Note: "Replacing this key preserves its model settings and fallback position."})
	}
	_, backend, brave, tavily, _ := s.m.WebSearch()
	for _, p := range []struct{ id, name, key string }{{"brave", "Brave Search", brave}, {"tavily", "Tavily Search", tavily}} {
		result = append(result, accountConnection{ID: p.id, Kind: "search", Name: p.name, Enabled: backend == p.id || backend == p.id+"-free", Configured: p.key != "", Fields: []accountCredentialField{{"api_key", p.key != ""}}, Note: "Saving a valid key tests and enables this web search backend."})
	}
	writeJSON(w, 200, map[string]any{"connections": result})
}

func (s *Server) saveAccountConnection(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	var in struct {
		Kind     string            `json:"kind"`
		ID       string            `json:"id"`
		Fields   map[string]string `json:"fields"`
		Endpoint string            `json:"endpoint"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "Invalid connection details")
		return
	}
	switch in.Kind {
	case "llm":
		id, _ := strconv.ParseInt(in.ID, 10, 64)
		p, err := pg.ProfileByID(id)
		if err != nil || p == nil {
			writeErr(w, 404, "Model profile not found")
			return
		}
		key := strings.TrimSpace(in.Fields["api_key"])
		if key == "" {
			writeErr(w, 400, "Enter the replacement API key")
			return
		}
		p.APIKey = key
		if _, err := pg.SaveProfile(p); err != nil {
			writeErr(w, 500, "Could not store model key")
			return
		}
		s.llmHealth.Reset(id)
		s.invalidateProfileAgents()
		s.reapplyActiveProfile()
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": p.IsDefault, "message": "Model key saved; fallback order preserved."})
	case "mcp":
		id, _ := strconv.ParseInt(in.ID, 10, 64)
		servers, _ := pg.ListMCP()
		var target *db.MCPServer
		for _, m := range servers {
			if m.ID == id {
				target = m
				break
			}
		}
		if target == nil {
			writeErr(w, 404, "MCP connection not found")
			return
		}
		if target.Name == "rea" || target.Name == "gmail" || target.Name == "metasploit-official" || strings.HasPrefix(target.Name, "gmail-") || strings.HasPrefix(target.Name, "telegram-") || strings.HasPrefix(target.Name, "atomicmail-") {
			writeErr(w, 400, "This credential is managed by its account or internal service")
			return
		}
		fields := mcpCredentialFields(target)
		allowed := map[string]bool{}
		for _, f := range fields {
			allowed[f.Key] = true
		}
		env := jsonStrMap(target.Env)
		if env == nil {
			env = map[string]string{}
		}
		for key, value := range in.Fields {
			if !allowed[key] {
				writeErr(w, 400, "Unexpected credential field")
				return
			}
			if strings.TrimSpace(value) != "" {
				if strings.EqualFold(key, "Authorization") && !strings.HasPrefix(value, "Bearer ") {
					value = "Bearer " + value
				}
				env[key] = value
			}
		}
		for _, f := range fields {
			if !credentialPresent(env[f.Key]) {
				writeErr(w, 400, "Complete all credential fields")
				return
			}
		}
		if in.Endpoint != "" {
			u, e := url.Parse(strings.TrimSpace(in.Endpoint))
			if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
				writeErr(w, 400, "Enter a valid MCP HTTP endpoint")
				return
			}
			target.URL = u.String()
		}
		if target.Transport != "stdio" && target.URL == "" {
			writeErr(w, 400, "This connection also needs its MCP endpoint URL")
			return
		}
		target.Env, _ = json.Marshal(env)
		target.Enabled = false
		if _, err := pg.SaveMCP(target); err != nil {
			writeErr(w, 500, "Could not store connection credentials")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
		defer cancel()
		if err := s.discoverAndCacheMCP(ctx, target); err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "enabled": false, "message": "Credentials saved, but the connection test failed. Check the token and endpoint; the MCP remains disabled."})
			return
		}
		target.Enabled = true
		if _, err := pg.SaveMCP(target); err != nil {
			writeErr(w, 500, "Could not enable connection")
			return
		}
		agents, _ := pg.ListAgents()
		for _, a := range agents {
			if a.Enabled {
				_ = pg.ToggleVisibility(a.ID, "mcp", id, true)
			}
		}
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": true, "message": "Connection verified and enabled for agents."})
	case "search":
		// Reuse the existing settings and probe handler, including its real API-key test.
		key := strings.TrimSpace(in.Fields["api_key"])
		if key == "" {
			writeErr(w, 400, "Enter a search API key")
			return
		}
		if in.ID != "brave" && in.ID != "tavily" {
			writeErr(w, 400, "Unknown search provider")
			return
		}
		if err := s.saveAccountSearchKey(r.Context(), in.ID, key); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": true, "message": "Search provider tested and enabled."})
	default:
		writeErr(w, 400, "Unknown connection type")
	}
}

func (s *Server) saveAccountSearchKey(ctx context.Context, provider, key string) error {
	return s.configureAccountSearch(ctx, provider, key)
}
