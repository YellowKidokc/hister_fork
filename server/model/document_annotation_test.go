// SPDX-License-Identifier: AGPL-3.0-or-later

package model_test

import (
	"testing"

	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestDocumentAnnotationsAreScopedAndUpdated(t *testing.T) {
	testutil.InitModelWithConfig(t, testutil.Config(t))
	ctx := t.Context()
	first := model.DocumentAnnotation{DocumentID: "https://example.org", UserID: 1, Notes: "private"}
	second := model.DocumentAnnotation{DocumentID: first.DocumentID, Tags: []string{"research"}, Summary: "first draft"}
	for _, annotation := range []*model.DocumentAnnotation{&first, &second} {
		if err := model.SaveDocumentAnnotation(ctx, annotation); err != nil {
			t.Fatal(err)
		}
	}
	second.Summary = "edited"
	if err := model.SaveDocumentAnnotation(ctx, &second); err != nil {
		t.Fatal(err)
	}
	for _, want := range []model.DocumentAnnotation{first, second, {UserID: 2, DocumentID: first.DocumentID}} {
		got, err := model.GetDocumentAnnotation(ctx, want.UserID, want.DocumentID)
		if err != nil || got.Notes != want.Notes || got.Summary != want.Summary || len(got.Tags) != len(want.Tags) {
			t.Fatalf("get annotation for user %d = %+v, %v, want %+v", want.UserID, got, err, want)
		}
	}
}
