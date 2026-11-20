// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateDocumentSummary(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected provider request: %s %s", r.Method, r.URL.Path)
		}
		var request struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Model != "test-model" ||
			len(request.Messages) != 2 || request.Messages[1].Role != "user" || len(request.Messages[1].Content) > 24000 {
			t.Errorf("invalid summary payload: %v", err)
		}
		io.WriteString(w, `{"choices":[{"message":{"content":"A generated draft."}}]}`)
	}))
	defer provider.Close()

	t.Setenv("TP_AI_BASE_URL", provider.URL+"/v1/")
	t.Setenv("TP_AI_MODEL", "test-model")
	endpoint, model := summaryService()
	got, err := generateDocumentSummary(t.Context(), endpoint, model, "test-key", strings.Repeat("é", 13000))
	if err != nil || got != "A generated draft." {
		t.Fatalf("summary = %q, %v", got, err)
	}
}
