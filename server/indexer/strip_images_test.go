// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestStripImagesOnNewCaptures(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	const raw = `<article><p>Article text</p><picture><source srcset="wide.webp"><img src="hero.png"></picture><svg><title>Diagram</title></svg></article>`
	old := pendingDocument("https://example.com/old-image")
	old.HTML = raw
	if err := idx.Add(old); err != nil {
		t.Fatal(err)
	}
	idx.Close()

	cfg.App.StripImages = true
	idx = newTestIndexer(t, cfg)
	defer idx.Close()
	fresh := pendingDocument("https://example.com/new-image")
	fresh.HTML = raw
	if err := idx.Add(fresh); err != nil {
		t.Fatal(err)
	}

	if got := idx.GetByDocID(document.GetDocID(0, old.URL)); got == nil || got.HTML != raw {
		t.Fatalf("existing HTML changed: %+v", got)
	}
	got := idx.GetByDocID(document.GetDocID(0, fresh.URL))
	if got == nil || !strings.Contains(got.HTML, "Article text") || strings.Contains(got.HTML, "hero.png") || strings.Contains(got.HTML, "<svg") {
		t.Fatalf("new HTML was not stripped correctly: %+v", got)
	}
	if got.Text != fresh.Text || got.FaviconKey == "" {
		t.Fatal("stripping HTML images changed the stored text or favicon")
	}
}
