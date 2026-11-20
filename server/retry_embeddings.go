// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"net/http"

	"github.com/asciimoo/nogon/server/model"
	"github.com/rs/zerolog/log"
)

func serveRetryEmbeddings(c *webContext) {
	if !canConfigure(c) {
		serve403(c)
		return
	}
	if !c.Indexer.SemanticSearchEnabled() {
		http.Error(c.Response, "semantic search is disabled", http.StatusConflict)
		return
	}
	queued, err := model.RequeueFailedEmbeddingJobs(c.Request.Context(), 100)
	if err != nil {
		log.Error().Err(err).Msg("failed to retry embedding jobs")
		serve500(c)
		return
	}
	c.JSON(map[string]int64{"queued": queued})
}
