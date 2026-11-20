// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/asciimoo/nogon/config"
)

func researchIntakeUpstream(cfg *config.Config) *url.URL {
	// The standalone control center has one private library. Multi-user
	// Nogon installations continue to use their owner-scoped research jobs.
	if cfg.App.UserHandling || cfg.App.Public {
		return nil
	}
	u, err := url.Parse(strings.TrimSpace(os.Getenv("NOGON_RESEARCH_UPSTREAM")))
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") || (u.Path != "/" && u.Path != "") {
		return nil
	}
	return u
}

func legacyIntakeClosed(c *webContext) bool {
	if researchIntakeUpstream(c.Config) == nil {
		return false
	}
	http.Error(c.Response, "new submissions use the research control center at "+c.Config.BasePathPrefix()+"/research/", http.StatusConflict)
	return true
}

func serveResearchIntake(c *webContext) {
	target := researchIntakeUpstream(c.Config)
	if target == nil {
		http.NotFound(c.Response, c.Request)
		return
	}
	if !canWrite(c) {
		serve403(c)
		return
	}
	suffix := strings.TrimPrefix(c.Request.URL.Path, "/research")
	switch {
	case c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead:
		if suffix != "/" && suffix != "/app.js" && suffix != "/style.css" && !strings.HasPrefix(suffix, "/v1/") {
			http.NotFound(c.Response, c.Request)
			return
		}
	case c.Request.Method == http.MethodPost:
		if !strings.HasPrefix(suffix, "/v1/") {
			http.NotFound(c.Response, c.Request)
			return
		}
	default:
		c.Response.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if path.Clean(suffix) != suffix {
		http.NotFound(c.Response, c.Request)
		return
	}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(request *httputil.ProxyRequest) {
			request.Out.URL.Path = suffix
			request.Out.URL.RawPath = ""
			request.SetURL(target)
			// The control center validates an API request's browser Origin
			// against this host. Keep it while routing to the internal address.
			request.Out.Host = request.In.Host
			request.Out.Header.Del("Cookie")
			request.Out.Header.Del("X-Access-Token")
			request.Out.Header.Del("X-CSRF-Token")
			request.Out.Header.Del("Referer")
		},
		ModifyResponse: func(response *http.Response) error {
			response.Header.Del("Set-Cookie")
			response.Header.Set("Cache-Control", "no-store")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, _ error) {
			http.Error(w, "research control center is unavailable", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(c.Response, c.Request)
}
