// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/testutil"
)

func TestUnifiedResearchIntakePreservesLegacyReads(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Access-Token") != "" || r.Header.Get("Cookie") != "" {
			t.Error("Nogon credentials were forwarded to the research service")
		}
		if r.Method == http.MethodPost {
			if r.URL.Path != "/v1/jobs" || r.Header.Get("Authorization") != "Bearer research-token" {
				t.Errorf("unexpected control center request: %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusCreated)
		}
		_, _ = io.WriteString(w, "control center")
	}))
	defer upstream.Close()
	cfg, handler := newTokenTestServer(t, false)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "nogon://", "X-Access-Token": cfg.App.AccessToken}
	legacy := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/research/jobs", strings.NewReader(`{"name":"Existing project"}`), headers)
	if legacy.Code != http.StatusCreated {
		t.Fatalf("legacy job creation returned %d: %s", legacy.Code, legacy.Body)
	}

	t.Setenv("NOGON_RESEARCH_UPSTREAM", upstream.URL)
	redirect := testutil.ServeHTTP(t, handler, http.MethodGet, "/research", nil, headers)
	if redirect.Code != http.StatusTemporaryRedirect || redirect.Header().Get("Location") != "/research/" {
		t.Fatalf("research redirect: %d %s", redirect.Code, redirect.Header())
	}
	page := testutil.ServeHTTP(t, handler, http.MethodGet, "/research/", nil, headers)
	if page.Code != http.StatusOK || page.Body.String() != "control center" {
		t.Fatalf("proxied page: %d %s", page.Code, page.Body)
	}
	if anonymous := testutil.ServeHTTP(t, handler, http.MethodGet, "/research/v1/jobs", nil, nil); anonymous.Code != http.StatusForbidden {
		t.Fatalf("anonymous control center access: %d", anonymous.Code)
	}
	headers["Authorization"] = "Bearer research-token"
	create := testutil.ServeHTTP(t, handler, http.MethodPost, "/research/v1/jobs", strings.NewReader(`{"name":"New project"}`), headers)
	if create.Code != http.StatusCreated {
		t.Fatalf("control center job creation: %d %s", create.Code, create.Body)
	}
	legacyList := testutil.ServeHTTP(t, handler, http.MethodGet, "/api/research/jobs", nil, headers)
	if legacyList.Code != http.StatusOK || !strings.Contains(legacyList.Body.String(), "Existing project") {
		t.Fatalf("legacy list: %d %s", legacyList.Code, legacyList.Body)
	}
	legacyWrite := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/research/jobs", strings.NewReader(`{"name":"Other project"}`), headers)
	if legacyWrite.Code != http.StatusConflict {
		t.Fatalf("legacy creation returned %d after consolidation", legacyWrite.Code)
	}
}
