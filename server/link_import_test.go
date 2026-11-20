package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/asciimoo/nogon/server/testutil"
)

func TestParseLinkText(t *testing.T) {
	got := parseLinkText("https://a.example/x\n  http://b.example/y, ftp://no.example\nnot a link <https://c.example/z>")
	want := []string{"https://a.example/x", "http://b.example/y", "https://c.example/z"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func postLinkImport(t *testing.T, f *researchFixture, fields map[string]string, fileName, fileBody string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if fileName != "" {
		part, err := w.CreateFormFile("file", fileName)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write([]byte(fileBody))
	}
	_ = w.Close()
	return testutil.ServeHTTP(t, f.handler, http.MethodPost, "/api/research/links", &buf, f.headers(map[string]string{"Content-Type": w.FormDataContentType()}))
}

func TestLinkImportFetchesAndLabelsPages(t *testing.T) {
	f := newResearchFixture(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<html><head><title>Page %s</title></head><body><p>Quantum coherence notes for %s.</p></body></html>", r.URL.Path, r.URL.Path)
	}))
	defer site.Close()

	csv := "title,link\nOne," + site.URL + "/one\nTwo," + site.URL + "/two\n"
	r := postLinkImport(t, f, map[string]string{"urls": site.URL + "/three\n" + site.URL + "/one", "label": "physics"}, "links.csv", csv)
	if r.Code != http.StatusAccepted {
		t.Fatalf("start import: %d %s", r.Code, r.Body)
	}
	var started linkImportSummary
	if err := json.Unmarshal(r.Body.Bytes(), &started); err != nil {
		t.Fatal(err)
	}
	if started.Total != 3 {
		t.Fatalf("expected 3 unique links, got %d", started.Total)
	}

	var job linkImportSummary
	deadline := time.Now().Add(60 * time.Second)
	for {
		r = testutil.ServeHTTP(t, f.handler, http.MethodGet, "/api/research/links/"+started.ID, nil, f.headers(nil))
		if r.Code != http.StatusOK {
			t.Fatalf("get import: %d %s", r.Code, r.Body)
		}
		if err := json.Unmarshal(r.Body.Bytes(), &job); err != nil {
			t.Fatal(err)
		}
		if job.Finished != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("import did not finish: %+v", job)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.Indexed != 3 || job.Failed != 0 {
		t.Fatalf("expected 3 indexed pages, got %+v", job)
	}

	r = testutil.ServeHTTP(t, f.handler, http.MethodGet, "/search?q="+"label:physics+coherence", nil, f.headers(nil))
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "/two") {
		t.Fatalf("labelled pages not searchable: %d %s", r.Code, r.Body)
	}

	// A second import of the same links skips them as already indexed.
	r = postLinkImport(t, f, map[string]string{"urls": site.URL + "/one"}, "", "")
	if r.Code != http.StatusAccepted {
		t.Fatalf("second import: %d %s", r.Code, r.Body)
	}
}

func TestLinkImportRejectsEmptyInput(t *testing.T) {
	f := newResearchFixture(t)
	r := postLinkImport(t, f, map[string]string{"urls": "no links here"}, "", "")
	if r.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", r.Code, r.Body)
	}
	r = testutil.ServeHTTP(t, f.handler, http.MethodGet, "/api/research/links/doesnotexist", nil, f.headers(nil))
	if r.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", r.Code)
	}
}
