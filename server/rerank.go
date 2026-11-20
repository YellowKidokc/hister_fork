// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"cmp"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/asciimoo/nogon/server/vectorstore"
)

func rerankEndpoint() string {
	endpoint := strings.TrimSpace(os.Getenv("NOGON_RERANK_ENDPOINT"))
	u, err := url.Parse(endpoint)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return endpoint
}

func serveRerank(c *webContext) {
	if !canWrite(c) {
		serve403(c)
		return
	}
	endpoint := rerankEndpoint()
	if endpoint == "" {
		http.Error(c.Response, "reranking is not configured", http.StatusServiceUnavailable)
		return
	}
	var request struct {
		Query string   `json:"query"`
		IDs   []string `json:"ids"`
	}
	if !decodeRequestJSON(c, &request) {
		return
	}
	query := strings.TrimSpace(request.Query)
	if query == "" || len(query) > 4096 || len(request.IDs) < 2 || len(request.IDs) > 30 {
		http.Error(c.Response, "provide a query and 2 to 30 document IDs", http.StatusBadRequest)
		return
	}
	passages := make([]string, 0, len(request.IDs))
	for _, id := range request.IDs {
		doc := c.Indexer.GetByDocID(id)
		if doc == nil || (c.Config.App.UserHandling && doc.UserID != 0 && doc.UserID != c.UserID) {
			http.Error(c.Response, "a selected document no longer exists or is inaccessible", http.StatusNotFound)
			return
		}
		passage := strings.TrimSpace(doc.Title + "\n" + doc.Text)
		if len(passage) > 3500 {
			end := 3500
			for !utf8.RuneStart(passage[end]) {
				end--
			}
			passage = passage[:end]
		}
		passages = append(passages, passage)
	}
	scores, err := vectorstore.Rerank(c.Request.Context(), endpoint, os.Getenv("NOGON_RERANK_MODEL"), os.Getenv("NOGON_RERANK_KEY"), query, passages)
	if err != nil {
		http.Error(c.Response, err.Error(), http.StatusBadGateway)
		return
	}
	order := make([]int, len(scores))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int { return cmp.Compare(scores[j], scores[i]) })
	ids := make([]string, len(order))
	for i, index := range order {
		ids[i] = request.IDs[index]
	}
	c.JSON(map[string][]string{"ids": ids})
}
