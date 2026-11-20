// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestMixedStorageCompression(t *testing.T) {
	cfg := testutil.Config(t)
	idx := newTestIndexer(t, cfg)
	legacy := pendingDocument("https://example.com/gzip")
	if err := idx.Add(legacy); err != nil {
		t.Fatal(err)
	}
	idx.Close()

	cfg.Indexer.Compression = config.CompressionZstd
	idx = newTestIndexer(t, cfg)
	fresh := pendingDocument("https://example.com/zstd")
	fresh.HTML = "<p>" + strings.Repeat("A new Zstandard document. ", 100) + "</p>"
	wantHTML := fresh.HTML
	if err := idx.Add(fresh); err != nil {
		t.Fatal(err)
	}
	assertStoredBlobs(t, idx, legacy.ID())
	found := idx.GetByDocID(fresh.ID())
	if found == nil || found.HTML != wantHTML {
		t.Fatalf("could not read new Zstandard document: %+v", found)
	}
	gzipPath, err := dataFilePath(idx.data.dir, htmlSubdir, found.HTMLKey)
	if err != nil {
		t.Fatal(err)
	}
	zstdPath := blobVariants(gzipPath)[1]
	if _, err := os.Stat(zstdPath); err != nil {
		t.Fatalf("Zstandard blob missing: %v", err)
	}
	if _, err := os.Stat(gzipPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected gzip copy: %v", err)
	}
	idx.Close()

	cfg.Indexer.Compression = config.CompressionGzip
	idx = newTestIndexer(t, cfg)
	defer idx.Close()
	if doc := idx.GetByDocID(fresh.ID()); doc == nil || doc.HTML != wantHTML {
		t.Fatalf("Zstandard blob could not be read with gzip configured: %+v", doc)
	}
	assertStoredBlobs(t, idx, legacy.ID())
	if _, _, err := idx.CleanupDataFiles(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(zstdPath); err != nil {
		t.Fatalf("cleanup removed referenced Zstandard blob: %v", err)
	}
	if err := idx.Delete(fresh.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(zstdPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleting the last reference did not remove its blob: %v", err)
	}
}
