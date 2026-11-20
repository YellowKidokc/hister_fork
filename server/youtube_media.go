// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"errors"
	"net/http"
	"os"
	"os/exec"

	"github.com/asciimoo/nogon/internal/youtubemedia"
)

func serveYouTubeMedia(c *webContext) {
	if !canWrite(c) || !c.Config.App.YouTubeDownloads {
		serve403(c)
		return
	}
	doc := requestedDocument(c, c.Request.URL.Query().Get("url"))
	if doc == nil || doc.UserID != c.UserID || youtubemedia.WatchURL(doc.URL) == "" {
		http.Error(c.Response, "owned YouTube document not found", http.StatusNotFound)
		return
	}
	base := c.Config.FullPath("")
	id := doc.ID()
	path := youtubemedia.FilePath(base, id)

	switch c.Request.Method {
	case http.MethodPost:
		binary, err := exec.LookPath("yt-dlp")
		if err != nil {
			http.Error(c.Response, "yt-dlp is required on the Nogon server", http.StatusServiceUnavailable)
			return
		}
		err = youtubemedia.Download(c.Request.Context(), binary, path, doc.URL, func() bool {
			return c.Indexer.GetByDocID(id) != nil
		})
		if errors.Is(err, youtubemedia.ErrBusy) {
			http.Error(c.Response, err.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(c.Response, err.Error(), http.StatusBadGateway)
			return
		}
		c.Response.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if err := youtubemedia.Remove(base, id); err != nil {
			serve500(c)
			return
		}
		c.Response.WriteHeader(http.StatusNoContent)
	default:
		file, err := os.Open(path)
		if os.IsNotExist(err) {
			http.Error(c.Response, "no downloaded video for this document", http.StatusNotFound)
			return
		}
		if err != nil {
			serve500(c)
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			serve500(c)
			return
		}
		c.Response.Header().Set("Content-Type", "video/mp4")
		c.Response.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(c.Response, c.Request, "youtube.mp4", info.ModTime(), file)
	}
}
