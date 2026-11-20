// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"net/http"
	"strings"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/model"
	"github.com/rs/zerolog/log"
)

func serveDocumentAnnotations(c *webContext) {
	if !canWrite(c) {
		serve403(c)
		return
	}
	doc := requestedDocument(c, c.Request.URL.Query().Get("url"))
	if doc == nil {
		http.Error(c.Response, "document not found", http.StatusNotFound)
		return
	}

	ctx := c.Request.Context()
	id := document.GetDocID(doc.UserID, doc.URL)
	var annotation model.DocumentAnnotation
	var err error
	if c.Request.Method == http.MethodPost {
		if !decodeRequestJSON(c, &annotation) {
			return
		}
		if len(annotation.Tags) > 50 || len(annotation.Notes) > 65536 || len(annotation.Summary) > 65536 {
			http.Error(c.Response, "annotation exceeds size limit", http.StatusBadRequest)
			return
		}
		tags := make([]string, 0, len(annotation.Tags))
		seen := make(map[string]bool)
		for _, value := range annotation.Tags {
			tag := strings.TrimSpace(value)
			if len(tag) > 100 {
				http.Error(c.Response, "tag exceeds size limit", http.StatusBadRequest)
				return
			}
			key := strings.ToLower(tag)
			if tag != "" && !seen[key] {
				tags = append(tags, tag)
				seen[key] = true
			}
		}
		annotation.Tags = tags
		annotation.UserID = c.UserID
		annotation.DocumentID = id
		err = model.SaveDocumentAnnotation(ctx, &annotation)
	} else {
		annotation, err = model.GetDocumentAnnotation(ctx, c.UserID, id)
	}
	if err != nil {
		log.Error().Err(err).Msg("document annotation request failed")
		serve500(c)
		return
	}
	c.JSON(annotation)
}
