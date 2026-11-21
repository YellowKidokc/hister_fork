// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/testutil"
)

func TestVisualArchiveRoundTrip(t *testing.T) {
	cfg, handler := newPublicTokenTestServer(t)
	cfg.App.VisualArchives = true
	const page = "https://example.com/visual"
	endpoint := "/api/document/visual?url=" + url.QueryEscape(page)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "nogon://", "X-Access-Token": cfg.App.AccessToken}
	do := func(method, endpoint, body string, headers map[string]string) (int, []byte) {
		r := testutil.ServeHTTP(t, handler, method, endpoint, strings.NewReader(body), headers)
		return r.Code, r.Body.Bytes()
	}
	if status, body := do(http.MethodPost, "/api/add", `{"url":"https://example.com/visual","text":"A saved page"}`, headers); status != http.StatusCreated {
		t.Fatalf("indexing page returned %d: %s", status, body)
	}
	if status, _ := do(http.MethodPost, endpoint, `{"image":"data:image/jpeg;base64,PHNjcmlwdD4="}`, headers); status != http.StatusBadRequest {
		t.Fatalf("invalid JPEG returned %d", status)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil); err != nil {
		t.Fatal(err)
	}
	payload := `{"image":"data:image/jpeg;base64,` + base64.StdEncoding.EncodeToString(encoded.Bytes()) + `"}`
	if status, body := do(http.MethodPost, endpoint, payload, headers); status != http.StatusNoContent {
		t.Fatalf("saving image returned %d: %s", status, body)
	}
	if status, _ := do(http.MethodGet, endpoint, "", nil); status != http.StatusForbidden {
		t.Fatalf("anonymous image request returned %d", status)
	}
	if status, body := do(http.MethodGet, endpoint, "", headers); status != http.StatusOK || !bytes.Equal(body, encoded.Bytes()) {
		t.Fatalf("reading image returned %d with %d bytes", status, len(body))
	}
	if status, _ := do(http.MethodDelete, endpoint, "", headers); status != http.StatusNoContent {
		t.Fatalf("deleting image returned %d", status)
	}
	if status, _ := do(http.MethodGet, endpoint, "", headers); status != http.StatusNotFound {
		t.Fatalf("reading deleted image returned %d", status)
	}
}
