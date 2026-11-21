// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/testutil"
)

func TestOfflineReaderDownloadEscapesStoredText(t *testing.T) {
	cfg, handler := newPublicTokenTestServer(t)
	const documentURL = "https://example.com/reader"
	add := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/add", strings.NewReader(
		`{"url":"https://example.com/reader","title":"Offline reader","text":"A literal <script> tag."}`,
	), map[string]string{
		"Content-Type": "application/json", "X-Access-Token": cfg.App.AccessToken, "Origin": "nogon://",
	})
	if add.Code != http.StatusCreated {
		t.Fatalf("add returned %d: %s", add.Code, add.Body)
	}

	endpoint := "/api/document/reader?url=" + url.QueryEscape(documentURL)
	anonymous := testutil.ServeHTTP(t, handler, http.MethodGet, endpoint, nil, nil)
	if anonymous.Code != http.StatusForbidden {
		t.Fatalf("anonymous reader request returned %d", anonymous.Code)
	}
	reader := testutil.ServeHTTP(t, handler, http.MethodGet, endpoint, nil, map[string]string{
		"X-Access-Token": cfg.App.AccessToken,
	})
	if reader.Code != http.StatusOK || !strings.HasPrefix(reader.Header().Get("Content-Disposition"), "attachment") {
		t.Fatalf("reader returned %d and %s", reader.Code, reader.Header())
	}
	if body := reader.Body.String(); !strings.Contains(body, "&lt;script&gt;") || strings.Contains(body, "<script>") {
		t.Fatalf("reader did not escape stored text: %s", body)
	}
}
