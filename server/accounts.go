package server

import (
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

// Account credentials and Telegram authorization live in the private host
// service. Only this authenticated, same-origin API is exposed to the browser.
func (s *Server) accountsProxy(w http.ResponseWriter, r *http.Request) {
	upstream, token := os.Getenv("ARTEX_ACCOUNTS_URL"), os.Getenv("ARTEX_ACCOUNTS_TOKEN")
	if upstream == "" || token == "" {
		writeErr(w, http.StatusServiceUnavailable, "Accounts service is not configured. Contact the server administrator.")
		return
	}
	target, err := url.Parse(upstream)
	if err != nil || target.Scheme != "http" || target.Host == "" || target.User != nil {
		writeErr(w, http.StatusServiceUnavailable, "Accounts service configuration is invalid.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	proxy := httputil.NewSingleHostReverseProxy(target)
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		req.URL.Path = "/v1" + strings.TrimPrefix(r.URL.Path, "/api")
		req.URL.RawQuery = "" // Credentials and challenge identifiers never travel in URLs.
		req.Header.Del("Cookie")
		req.Header.Del("X-Forwarded-For")
		req.Header.Del("X-Real-IP")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Host = target.Host
	}
	proxy.Transport = &http.Transport{Proxy: nil, ResponseHeaderTimeout: 90 * time.Second}
	proxy.ModifyResponse = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusUnauthorized {
			_ = resp.Body.Close()
			body := `{"error":"Accounts service authentication needs attention. Contact the server administrator."}`
			resp.Body = io.NopCloser(strings.NewReader(body))
			resp.StatusCode = http.StatusServiceUnavailable
			resp.ContentLength = int64(len(body))
			resp.Header.Del("Content-Length")
			resp.Header.Set("Content-Type", "application/json")
		}
		resp.Header.Set("Cache-Control", "no-store")
		resp.Header.Del("Set-Cookie")
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		writeErr(w, http.StatusServiceUnavailable, "Accounts service is unavailable. Please retry shortly.")
	}
	defer proxy.Transport.(*http.Transport).CloseIdleConnections()
	proxy.ServeHTTP(w, r)
}
