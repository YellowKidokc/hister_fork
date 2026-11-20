// SPDX-License-Identifier: AGPL-3.0-or-later

package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"time"
)

var rerankHTTP = &http.Client{
	Timeout:       25 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Rerank returns one validated relevance score per passage, in input order.
func Rerank(ctx context.Context, endpoint, model, key, query string, passages []string) ([]float64, error) {
	payload := struct {
		Model     string   `json:"model,omitempty"`
		Query     string   `json:"query"`
		Documents []string `json:"documents"`
	}{model, query, passages}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := rerankHTTP.Do(req)
	if err != nil {
		return nil, errors.New("could not reach the reranking service")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("the reranking service rejected the request")
	}
	var result struct {
		Results []struct {
			Index *int     `json:"index"`
			Score *float64 `json:"relevance_score"`
		} `json:"results"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&result); err != nil || len(result.Results) != len(passages) {
		return nil, errors.New("the reranking service did not score every result")
	}
	scores := make([]float64, len(passages))
	seen := make([]bool, len(passages))
	for _, hit := range result.Results {
		if hit.Index == nil || hit.Score == nil || *hit.Index < 0 || *hit.Index >= len(scores) ||
			seen[*hit.Index] || math.IsInf(*hit.Score, 0) || math.IsNaN(*hit.Score) {
			return nil, errors.New("the reranking service returned invalid scores")
		}
		scores[*hit.Index] = *hit.Score
		seen[*hit.Index] = true
	}
	return scores, nil
}
