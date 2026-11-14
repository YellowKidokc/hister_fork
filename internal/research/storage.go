package research

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// StoragePlan is an immutable preview, not a filesystem operation or work item.
// It deliberately cannot represent database/index migration or source deletion.
type StoragePlan struct {
	ID          string            `json:"id" gorm:"primaryKey"`
	Status      string            `json:"status"`
	Destination string            `json:"destination"`
	DocumentIDs []string          `json:"document_ids" gorm:"serializer:json;type:jsonb"`
	Kinds       []string          `json:"kinds" gorm:"serializer:json;type:jsonb"`
	Items       []StoragePlanItem `json:"items" gorm:"serializer:json;type:jsonb"`
	TotalBytes  int64             `json:"total_bytes"`
	CreatedAt   time.Time         `json:"created_at"`
}

func (StoragePlan) TableName() string { return "tp_storage_plans" }

type StoragePlanItem struct {
	DocumentID     string `json:"document_id"`
	Name           string `json:"name"`
	Kind           string `json:"kind"`
	Source         string `json:"source"`
	Destination    string `json:"destination"`
	Bytes          int64  `json:"bytes"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
}
type StorageSelection struct {
	DocumentIDs []string `json:"document_ids"`
	Kinds       []string `json:"kinds"`
}

func (a *API) storageDestination() string {
	if a.VolumeOneFolder != "" {
		return a.VolumeOneFolder
	}
	return "/volume1/Theophysics/library"
}
func (a *API) storageInventory(w http.ResponseWriter, r *http.Request) {
	q := a.Store.DB.WithContext(r.Context()).Model(&Document{})
	if job := r.URL.Query().Get("job"); job != "" {
		q = q.Where("id IN (?)", a.Store.DB.Model(&Receipt{}).Select("document_id").Where("job_id = ?", job))
	}
	if cat := r.URL.Query().Get("category"); cat != "" {
		q = q.Where("id IN (?)", a.Store.DB.Model(&Membership{}).Select("document_id").Where("category_id = ?", cat))
	}
	switch days := r.URL.Query().Get("older_than_days"); days {
	case "", "0":
	case "60", "90":
		n := 60
		if days == "90" {
			n = 90
		}
		q = q.Where("created_at < ?", time.Now().Add(-time.Duration(n)*24*time.Hour))
	default:
		fail(w, errors.New("choose all ages, 60 days, or 90 days"))
		return
	}
	page := 0
	if value := r.URL.Query().Get("page"); value != "" {
		parsed, e := strconv.Atoi(value)
		if e != nil || parsed < 0 || parsed > 1000000 {
			fail(w, errors.New("invalid storage page"))
			return
		}
		page = parsed
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		fail(w, err)
		return
	}
	docs := []Document{}
	if err := q.Order("created_at DESC, id").Limit(100).Offset(page * 100).Find(&docs).Error; err != nil {
		fail(w, err)
		return
	}
	ids := []string{}
	for _, d := range docs {
		ids = append(ids, d.ID)
	}
	memberships := []Membership{}
	if len(ids) > 0 {
		if err := a.Store.DB.WithContext(r.Context()).Where("document_id IN ?", ids).Find(&memberships).Error; err != nil {
			fail(w, err)
			return
		}
	}
	jsonResponse(w, 200, map[string]any{"documents": docs, "memberships": memberships, "total": total, "page": page, "has_more": int64((page+1)*100) < total, "destination": a.storageDestination(), "mode": "preview_only", "automatic_cleanup": false, "kept_on_nvme": []string{"Programs and running services", "PostgreSQL metadata and job records", "Hister full-text search index"}})
}

func (s *Store) PreviewStorage(ctx context.Context, selection StorageSelection, destination string) (StoragePlan, error) {
	plan := StoragePlan{ID: id(), Status: "preview", Destination: destination, DocumentIDs: selection.DocumentIDs, Kinds: selection.Kinds, Items: []StoragePlanItem{}}
	if len(selection.DocumentIDs) == 0 || len(selection.DocumentIDs) > 100 {
		return plan, errors.New("select 1–100 documents for a move preview")
	}
	selected := map[string]bool{}
	for _, docID := range selection.DocumentIDs {
		if selected[docID] {
			return plan, errors.New("document is selected more than once")
		}
		selected[docID] = true
	}
	kinds := map[string]bool{}
	for _, kind := range selection.Kinds {
		if kind != "originals" && kind != "extracted_text" {
			return plan, errors.New("only research originals and extracted text can be selected; PostgreSQL and Hister stay on NVMe")
		}
		if kinds[kind] {
			return plan, errors.New("data type selected more than once")
		}
		kinds[kind] = true
	}
	if len(kinds) == 0 {
		return plan, errors.New("choose at least one research data type")
	}
	if !filepath.IsAbs(destination) || strings.ContainsAny(destination, "\r\n") {
		return plan, errors.New("the planned volume-one folder must be an absolute server-configured path")
	}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var docs []Document
		if e := tx.Where("id IN ?", selection.DocumentIDs).Order("created_at,id").Find(&docs).Error; e != nil {
			return e
		}
		if len(docs) != len(selected) {
			return errors.New("one or more selected documents no longer exist")
		}
		for _, d := range docs {
			add := func(kind, source, relative, expectedHash string) error {
				// Planning reads file metadata only. Reject aliases outside this store so
				// a preview cannot be mistaken for authority to move unrelated files.
				rel, e := filepath.Rel(s.Root, source)
				if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return errors.New("a selected file is outside the managed research store")
				}
				resolved, e := filepath.EvalSymlinks(source)
				if e != nil {
					return fmt.Errorf("%s is unavailable; refresh before preparing a move", d.Name)
				}
				resolvedRoot, e := filepath.EvalSymlinks(s.Root)
				if e != nil {
					return e
				}
				rel, e = filepath.Rel(resolvedRoot, resolved)
				if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return errors.New("a selected file resolves outside the managed research store")
				}
				info, e := os.Lstat(source)
				if e != nil || !info.Mode().IsRegular() {
					return fmt.Errorf("%s is unavailable or is not a regular file", d.Name)
				}
				if kind == "originals" && info.Size() != d.Size {
					return fmt.Errorf("%s has changed size; inspect its source before planning a move", d.Name)
				}
				item := StoragePlanItem{DocumentID: d.ID, Name: d.Name, Kind: kind, Source: source, Destination: filepath.Join(destination, relative), Bytes: info.Size(), ExpectedSHA256: expectedHash}
				plan.Items = append(plan.Items, item)
				plan.TotalBytes += item.Bytes
				return nil
			}
			if kinds["originals"] {
				if e := add("originals", d.Path, filepath.Join("objects", d.SHA256), d.SHA256); e != nil {
					return e
				}
			}
			if kinds["extracted_text"] && d.TextPath != "" {
				if e := add("extracted_text", d.TextPath, filepath.Join("text", d.ID+".txt"), ""); e != nil {
					return e
				}
			}
		}
		if len(plan.Items) == 0 {
			return errors.New("selected documents have no stored files of the requested type")
		}
		if e := tx.Create(&plan).Error; e != nil {
			return e
		}
		return audit(tx, "storage.preview_created", "", "", map[string]any{"plan_id": plan.ID, "documents": plan.DocumentIDs, "kinds": plan.Kinds, "file_count": len(plan.Items), "bytes": plan.TotalBytes, "destination": destination, "files_moved": 0})
	})
	return plan, err
}
func (a *API) previewStorage(w http.ResponseWriter, r *http.Request) {
	var selection StorageSelection
	if err := decode(w, r, &selection); err != nil {
		fail(w, err)
		return
	}
	plan, err := a.Store.PreviewStorage(r.Context(), selection, a.storageDestination())
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 201, plan)
}
func (a *API) storagePlans(w http.ResponseWriter, r *http.Request) {
	plans := []StoragePlan{}
	if err := a.Store.DB.WithContext(r.Context()).Order("created_at DESC").Limit(10).Find(&plans).Error; err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 200, plans)
}
