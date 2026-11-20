// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"image/jpeg"
	"net/http"
	"strings"
	"time"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/model"
	"gorm.io/gorm"
)

const maxArchiveBytes = 8 << 20

func visualArchiveBodyLimit(cfg *config.Config) int64 {
	return min(cfg.Server.MaxBatchBodyBytes(), 12<<20)
}

func serveVisualArchive(c *webContext) {
	if !canWrite(c) || !c.Config.App.VisualArchives {
		serve403(c)
		return
	}
	doc := requestedDocument(c, c.Request.URL.Query().Get("url"))
	if doc == nil || doc.Type != document.Web || doc.UserID != c.UserID {
		http.Error(c.Response, "owned web document not found", http.StatusNotFound)
		return
	}

	ctx := c.Request.Context()
	id := document.GetDocID(doc.UserID, doc.URL)
	switch c.Request.Method {
	case http.MethodPost:
		var request struct {
			Image string `json:"image"`
		}
		if !decodeRequestJSON(c, &request) {
			return
		}
		payload, ok := strings.CutPrefix(request.Image, "data:image/jpeg;base64,")
		if !ok || len(payload) > base64.StdEncoding.EncodedLen(maxArchiveBytes) {
			http.Error(c.Response, "expected a JPEG screenshot up to 8 MiB", http.StatusBadRequest)
			return
		}
		image, err := base64.StdEncoding.DecodeString(payload)
		if err != nil || len(image) > maxArchiveBytes || !validArchiveJPEG(image) {
			http.Error(c.Response, "invalid JPEG screenshot", http.StatusBadRequest)
			return
		}
		err = model.SaveVisualArchive(ctx, &model.VisualArchive{
			UserID: c.UserID, DocumentID: id, Image: image, CapturedAt: time.Now().UTC(),
		})
		if err != nil {
			serve500(c)
			return
		}
		c.Response.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := model.DeleteVisualArchive(ctx, c.UserID, id); err != nil {
			serve500(c)
			return
		}
		c.Response.WriteHeader(http.StatusNoContent)
	default:
		archive, err := model.ReadVisualArchive(ctx, c.UserID, id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(c.Response, "no visual archive for this page", http.StatusNotFound)
			return
		}
		if err != nil {
			serve500(c)
			return
		}
		c.Response.Header().Set("Content-Type", "image/jpeg")
		c.Response.Header().Set("X-Content-Type-Options", "nosniff")
		c.Response.Header().Set("Content-Length", fmt.Sprint(len(archive.Image)))
		if c.Request.Method != http.MethodHead {
			_, _ = c.Response.Write(archive.Image)
		}
	}
}

func validArchiveJPEG(image []byte) bool {
	dimensions, err := jpeg.DecodeConfig(bytes.NewReader(image))
	if err != nil || dimensions.Width <= 0 || dimensions.Height <= 0 || int64(dimensions.Width)*int64(dimensions.Height) > 24_000_000 {
		return false
	}
	_, err = jpeg.Decode(bytes.NewReader(image))
	return err == nil
}
