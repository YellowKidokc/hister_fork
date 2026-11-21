// SPDX-License-Identifier: AGPL-3.0-or-later

package model_test

import (
	"testing"
	"time"

	"github.com/asciimoo/nogon/server/model"
	"github.com/asciimoo/nogon/server/testutil"
)

func TestRequeueFailedEmbeddingJobsLimitsWork(t *testing.T) {
	testutil.InitModel(t)
	now := time.Now().UTC()
	jobs := []model.EmbeddingJob{
		{DocID: "old-failure", Status: model.EmbeddingJobFailed, Attempts: 5, LastError: "offline", UpdatedAt: now.Add(-time.Hour)},
		{DocID: "new-failure", Status: model.EmbeddingJobFailed, Attempts: 5, UpdatedAt: now},
		{DocID: "running", Status: model.EmbeddingJobInProgress, Attempts: 1, UpdatedAt: now},
	}
	if err := model.DB.Create(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	queued, err := model.RequeueFailedEmbeddingJobs(t.Context(), 1)
	if err != nil || queued != 1 {
		t.Fatalf("queued = %d, %v", queued, err)
	}
	for i, want := range []string{model.EmbeddingJobPending, model.EmbeddingJobFailed, model.EmbeddingJobInProgress} {
		var got model.EmbeddingJob
		if err := model.DB.Take(&got, "doc_id = ?", jobs[i].DocID).Error; err != nil {
			t.Fatal(err)
		}
		if got.Status != want {
			t.Fatalf("%s status = %s, want %s", got.DocID, got.Status, want)
		}
		if i == 0 && (got.Attempts != 0 || got.LastError != "offline") {
			t.Fatalf("retry reset the wrong fields: %+v", got)
		}
	}
}
