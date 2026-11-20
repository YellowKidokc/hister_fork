// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/testutil"
)

func TestYouTubeDownloadsOnlyOnRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the yt-dlp test double uses a shell script")
	}
	dir := t.TempDir()
	fake := filepath.Join(dir, "yt-dlp")
	marker := filepath.Join(dir, "downloaded")
	t.Setenv("NOGON_TEST_YTDLP_MARKER", marker)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(fake, []byte(`#!/bin/sh
set -eu
while [ "$1" != "--output" ]; do shift; done
output="$2"
shift 2
[ "$1" = "--" ]
[ "$2" = "https://www.youtube.com/watch?v=aqz-KE-bpKQ" ]
printf 'mock MP4 content' > "$output"
touch "$NOGON_TEST_YTDLP_MARKER"
`), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, handler := newPublicTokenTestServer(t)
	cfg.App.YouTubeDownloads = true
	const page = "https://youtu.be/aqz-KE-bpKQ?list=ignored"
	endpoint := "/api/document/youtube?url=" + url.QueryEscape(page)
	headers := map[string]string{"Content-Type": "application/json", "Origin": "nogon://", "X-Access-Token": cfg.App.AccessToken}
	add := testutil.ServeHTTP(t, handler, http.MethodPost, "/api/add", strings.NewReader(
		`{"url":"https://youtu.be/aqz-KE-bpKQ?list=ignored","text":"An indexed video"}`,
	), headers)
	if add.Code != http.StatusCreated {
		t.Fatalf("add: %d %s", add.Code, add.Body)
	}
	if check := testutil.ServeHTTP(t, handler, http.MethodHead, endpoint, nil, headers); check.Code != http.StatusNotFound {
		t.Fatalf("status check: %d", check.Code)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("the downloader ran before a user request: %v", err)
	}
	if download := testutil.ServeHTTP(t, handler, http.MethodPost, endpoint, nil, headers); download.Code != http.StatusNoContent {
		t.Fatalf("download: %d %s", download.Code, download.Body)
	}
	if anonymous := testutil.ServeHTTP(t, handler, http.MethodGet, endpoint, nil, nil); anonymous.Code != http.StatusForbidden {
		t.Fatalf("anonymous stream: %d", anonymous.Code)
	}
	headers["Range"] = "bytes=0-3"
	stream := testutil.ServeHTTP(t, handler, http.MethodGet, endpoint, nil, headers)
	if stream.Code != http.StatusPartialContent || stream.Body.String() != "mock" {
		t.Fatalf("stream: %d %s", stream.Code, stream.Body)
	}
}
