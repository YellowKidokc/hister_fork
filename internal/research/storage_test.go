package research

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStoragePreviewPreservesSourceIdentityAndFiles(t *testing.T) {
	s := testStore(t)
	j := mustJob(t, s)
	source := "Stored research source that stays exactly where it was."
	receipt := ingest(t, s, j, "original.txt", source)
	copy := ingest(t, s, j, "copy.txt", source)
	if copy.DocumentID != receipt.DocumentID {
		t.Fatal("duplicate identity changed")
	}
	var original Document
	if err := s.DB.First(&original, "id = ?", receipt.DocumentID).Error; err != nil {
		t.Fatal(err)
	}
	textBefore, err := os.ReadFile(original.TextPath)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "not-created", "library")
	plan, err := s.PreviewStorage(context.Background(), StorageSelection{[]string{original.ID}, []string{"originals", "extracted_text"}}, destination)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != "preview" || len(plan.Items) != 2 || len(plan.DocumentIDs) != 1 {
		t.Fatal(plan)
	}
	if plan.TotalBytes != int64(len(source)+len(textBefore)) {
		t.Fatal("incorrect planned byte count", plan.TotalBytes)
	}
	var after Document
	if err = s.DB.First(&after, "id = ?", original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if after.Path != original.Path || after.TextPath != original.TextPath || after.Status != original.Status || after.SHA256 != original.SHA256 {
		t.Fatal("preview changed document state")
	}
	raw, err := os.ReadFile(original.Path)
	if err != nil || string(raw) != source {
		t.Fatal("source bytes changed", err)
	}
	textAfter, err := os.ReadFile(original.TextPath)
	if err != nil || string(textAfter) != string(textBefore) {
		t.Fatal("derived text changed", err)
	}
	if _, err = os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("preview created a destination", err)
	}
	for _, item := range plan.Items {
		if !strings.HasPrefix(item.Destination, destination+string(filepath.Separator)) {
			t.Fatal(item)
		}
		if item.Kind == "originals" && item.ExpectedSHA256 != original.SHA256 {
			t.Fatal("original hash lost")
		}
	}
	var stored StoragePlan
	if err = s.DB.First(&stored, "id = ?", plan.ID).Error; err != nil || len(stored.Items) != 2 {
		t.Fatal("preview not durable", err)
	}
	var event Event
	if err = s.DB.First(&event, "kind = ?", "storage.preview_created").Error; err != nil {
		t.Fatal(err)
	}
	if event.Details["files_moved"] != float64(0) {
		t.Fatal("incorrect movement receipt", event.Details)
	}
}
func TestStorageRejectsUnsupportedOrAmbiguousSelections(t *testing.T) {
	s := testStore(t)
	j := mustJob(t, s)
	r := ingest(t, s, j, "data.txt", "Original document")
	for name, selection := range map[string]StorageSelection{
		"empty":              {nil, []string{"originals"}},
		"no types":           {[]string{r.DocumentID}, nil},
		"duplicate document": {[]string{r.DocumentID, r.DocumentID}, []string{"originals"}},
		"database":           {[]string{r.DocumentID}, []string{"postgres"}},
		"index":              {[]string{r.DocumentID}, []string{"hister"}},
		"duplicate type":     {[]string{r.DocumentID}, []string{"originals", "originals"}},
		"missing document":   {[]string{"missing"}, []string{"originals"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := s.PreviewStorage(context.Background(), selection, "/volume1/Theophysics/library"); err == nil {
				t.Fatal("invalid selection accepted")
			}
		})
	}
	if count(t, s, &StoragePlan{}) != 0 {
		t.Fatal("invalid selection saved a plan")
	}
}
func TestStorageBlocksUnavailableOrChangedSources(t *testing.T) {
	for _, scenario := range []string{"missing", "changed", "outside"} {
		t.Run(scenario, func(t *testing.T) {
			s := testStore(t)
			j := mustJob(t, s)
			r := ingest(t, s, j, "data.txt", "Original bytes")
			var doc Document
			s.DB.First(&doc, "id = ?", r.DocumentID)
			switch scenario {
			case "missing":
				if err := os.Remove(doc.Path); err != nil {
					t.Fatal(err)
				}
			case "changed":
				if err := os.WriteFile(doc.Path, []byte("A changed source of a different size"), 0600); err != nil {
					t.Fatal(err)
				}
			case "outside":
				outside := filepath.Join(t.TempDir(), "unrelated.txt")
				if err := os.WriteFile(outside, []byte("Original bytes"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(doc.Path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, doc.Path); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.PreviewStorage(context.Background(), StorageSelection{[]string{doc.ID}, []string{"originals"}}, "/volume1/Theophysics/library"); err == nil {
				t.Fatal("unavailable or unowned source accepted")
			}
			if count(t, s, &StoragePlan{}) != 0 {
				t.Fatal("failed plan was saved")
			}
		})
	}
}
func TestStorageTextOnlyRequiresStoredText(t *testing.T) {
	s := testStore(t)
	j := mustJob(t, s)
	r := ingest(t, s, j, "binary.bin", string([]byte{0xff, 0xfe}))
	if _, err := s.PreviewStorage(context.Background(), StorageSelection{[]string{r.DocumentID}, []string{"extracted_text"}}, "/volume1/Theophysics/library"); err == nil {
		t.Fatal("missing extracted text accepted")
	}
	plan, err := s.PreviewStorage(context.Background(), StorageSelection{[]string{r.DocumentID}, []string{"originals"}}, "/volume1/Theophysics/library")
	if err != nil || len(plan.Items) != 1 {
		t.Fatal("preserved binary originals should be selectable", err)
	}
}
func TestStorageInventoryFiltersAndAuthentication(t *testing.T) {
	s := testStore(t)
	j1 := mustJob(t, s)
	j2 := mustJob(t, s)
	first := ingest(t, s, j1, "new.txt", "New information")
	second := ingest(t, s, j2, "old.txt", "Old physics")
	s.DB.Model(&Document{}).Where("id = ?", second.DocumentID).Update("created_at", time.Now().Add(-100*24*time.Hour))
	if err := s.DB.Create(&Membership{DocumentID: second.DocumentID, CategoryID: "physics", Source: "user"}).Error; err != nil {
		t.Fatal(err)
	}
	api := &API{Store: s, Token: strings.Repeat("s", 32)}
	server := httptest.NewServer(api.Handler())
	defer server.Close()
	get := func(path string, authorized bool) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest("GET", server.URL+path, nil)
		if authorized {
			req.Header.Set("Authorization", "Bearer "+api.Token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, result
	}
	status, _ := get("/v1/storage", false)
	if status != 401 {
		t.Fatal(status)
	}
	for _, query := range []string{"?older_than_days=90", "?category=physics", "?job=" + j2.ID} {
		status, result := get("/v1/storage"+query, true)
		if status != 200 || result["total"] != float64(1) {
			t.Fatal(status, result)
		}
		docs := result["documents"].([]any)
		if docs[0].(map[string]any)["id"] != second.DocumentID {
			t.Fatal("wrong scoped document", first, result)
		}
		if result["mode"] != "preview_only" || result["automatic_cleanup"] != false {
			t.Fatal("misleading storage capability")
		}
	}
	for _, query := range []string{"?page=0garbage", "?page=-1", "?older_than_days=1"} {
		if status, _ := get("/v1/storage"+query, true); status != 400 {
			t.Fatal("invalid filter accepted", query, status)
		}
	}
}
