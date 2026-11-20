// SPDX-License-Identifier: AGPL-3.0-or-later

package vectorstore

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestRerankRestoresInputScoreOrder(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Query     string   `json:"query"`
			Documents []string `json:"documents"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Query != "timeline" ||
			!slices.Equal(request.Documents, []string{"one", "two"}) || r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("unexpected rerank request: %+v, %v", request, err)
		}
		io.WriteString(w, `{"results":[{"index":1,"relevance_score":0.9},{"index":0,"relevance_score":0.1}]}`)
	}))
	defer provider.Close()

	got, err := Rerank(t.Context(), provider.URL, "mock", "test", "timeline", []string{"one", "two"})
	if err != nil || !slices.Equal(got, []float64{0.1, 0.9}) {
		t.Fatalf("scores: %+v, %v", got, err)
	}
}
