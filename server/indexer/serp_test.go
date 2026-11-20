// SPDX-License-Identifier: AGPL-3.0-or-later

package indexer

import (
	"testing"

	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestSearchResultSnippetsPreserveFullPages(t *testing.T) {
	idx := newTestIndexer(t, testutil.Config(t))
	defer idx.Close()

	for _, batched := range []bool{false, true} {
		page := "https://example.com/stored"
		add := idx.Add
		var batch *MultiBatch
		if batched {
			page = "https://example.com/staged"
			batch = idx.NewMultiBatch()
			add = batch.Add
			defer batch.Discard()
		}
		for _, doc := range []*document.Document{
			{URL: page, Text: "Complete page text.", Processed: true},
			{URL: page, Text: "Short search snippet.", Processed: true, SummaryOnly: true},
		} {
			if err := add(doc); err != nil {
				t.Fatal(err)
			}
		}
		if batch != nil {
			if err := batch.Save(); err != nil {
				t.Fatal(err)
			}
		}
		if doc := idx.GetByURLAndUser(page, 0); doc == nil || doc.Text != "Complete page text." {
			t.Fatalf("search snippet replaced full page: %+v", doc)
		}
	}
}
