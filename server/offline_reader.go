// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"html/template"
	"net/http"
	"strings"
)

var offlineReaderPage = template.Must(template.New("reader").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'">
<title>{{.Title}}</title>
<style>
:root { color-scheme: light dark; }
body { max-width: 68ch; margin: 3rem auto; padding: 0 1rem; font: 1.125rem/1.65 system-ui, sans-serif; }
p { white-space: pre-wrap; overflow-wrap: anywhere; }
small { overflow-wrap: anywhere; }
</style>
</head>
<body><main>
<h1>{{.Title}}</h1>
<small>{{.URL}}</small>
{{range .Paragraphs}}<p>{{.}}</p>{{end}}
</main></body></html>`))

func serveOfflineReader(c *webContext) {
	if !canWrite(c) {
		serve403(c)
		return
	}
	doc := requestedDocument(c, c.Request.URL.Query().Get("url"))
	if doc == nil {
		http.Error(c.Response, "document not found", http.StatusNotFound)
		return
	}
	text := strings.TrimSpace(doc.Text)
	if text == "" {
		http.Error(c.Response, "document has no indexed text", http.StatusUnprocessableEntity)
		return
	}
	title := doc.Title
	if title == "" {
		title = doc.URL
	}
	page := struct {
		Title, URL string
		Paragraphs []string
	}{
		Title: title, URL: doc.URL,
		Paragraphs: strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n"),
	}
	c.Response.Header().Set("Content-Disposition", `attachment; filename="nogon-reader.html"`)
	c.Response.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Response.Header().Set("X-Content-Type-Options", "nosniff")
	_ = offlineReaderPage.Execute(c.Response, page)
}
