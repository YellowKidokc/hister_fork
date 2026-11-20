// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/asciimoo/nogon/server/testutil"
)

func TestHomeChatUsesIndexedSourcesOnlyOnRequest(t *testing.T) {
	var providerCalls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		body, err := io.ReadAll(r.Body)
		if err != nil || r.URL.Path != "/v1/chat/completions" || !strings.Contains(string(body), "distinctive canvas pigment") {
			t.Errorf("unexpected provider request: %s %s %v", r.Method, r.URL.Path, err)
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"The source mentions pigment [1]."}}]}`)
	}))
	defer provider.Close()
	t.Setenv("TP_AI_BASE_URL", provider.URL+"/v1")
	t.Setenv("TP_AI_MODEL", "mock")
	cfg, handler := newPublicTokenTestServer(t)
	headers := map[string]string{"X-Access-Token": cfg.App.AccessToken, "Content-Type": "application/json", "Origin": "nogon://"}
	add := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/add", strings.NewReader(
		`{"url":"https://example.com/pigment","title":"Pigment article","text":"A distinctive canvas pigment."}`,
	), headers)
	if add.Code != http.StatusCreated {
		t.Fatalf("index: %d %s", add.Code, add.Body)
	}
	const question = `{"messages":[{"role":"user","content":"What does the source mention?"}],"search":"pigment"}`
	denied := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/ai/chat", strings.NewReader(question), nil)
	if denied.Code != http.StatusForbidden || providerCalls.Load() != 0 {
		t.Fatalf("anonymous chat: %d, provider calls: %d", denied.Code, providerCalls.Load())
	}
	reply := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/ai/chat", strings.NewReader(question), headers)
	if reply.Code != http.StatusOK || !strings.Contains(reply.Body.String(), "https://example.com/pigment") ||
		strings.Contains(reply.Body.String(), "distinctive canvas pigment") || providerCalls.Load() != 1 {
		t.Fatalf("chat: %d %s, provider calls: %d", reply.Code, reply.Body, providerCalls.Load())
	}
}
