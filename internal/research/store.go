package research

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

type Store struct {
	DB   *gorm.DB
	Root string
}

func Open(ctx context.Context, dsn, root string) (*Store, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	for _, p := range []string{"objects", "text", "incoming"} {
		if err = os.MkdirAll(filepath.Join(root, p), 0700); err != nil {
			return nil, err
		}
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(3)
	if err = sqlDB.PingContext(ctx); err != nil {
		return nil, err
	}
	// Version one uses dedicated tp_ tables and never changes Hister's tables.
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if e := tx.Exec("SELECT pg_advisory_xact_lock(816492135)").Error; e != nil {
			return e
		}
		if e := tx.AutoMigrate(&Job{}, &Document{}, &Receipt{}, &Category{}, &Membership{}, &Proposal{}, &Duplicate{}, &Task{}, &Event{}, &Export{}, &StoragePlan{}); e != nil {
			return e
		}
		if e := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&DefaultCategories).Error; e != nil {
			return e
		}
		for _, fk := range [][4]string{
			{"tp_receipts", "job_id", "tp_jobs", "id"},
			{"tp_proposals", "job_id", "tp_jobs", "id"},
			{"tp_proposals", "document_id", "tp_documents", "id"},
			{"tp_proposals", "category_id", "tp_categories", "id"},
			{"tp_memberships", "document_id", "tp_documents", "id"},
			{"tp_memberships", "category_id", "tp_categories", "id"},
			{"tp_duplicates", "document_id", "tp_documents", "id"},
			{"tp_duplicates", "candidate_id", "tp_documents", "id"},
			{"tp_index_tasks", "document_id", "tp_documents", "id"},
		} {
			name := fk[0] + "_" + fk[1] + "_fk"
			statement := fmt.Sprintf(`DO $migration$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = '%s' AND conrelid = '%s'::regclass) THEN ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s) ON DELETE RESTRICT; END IF; END $migration$;`, name, fk[0], fk[0], name, fk[1], fk[2], fk[3])
			if e := tx.Exec(statement).Error; e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Store{db, root}, nil
}
func (s *Store) Close() error {
	db, err := s.DB.DB()
	if err != nil {
		return err
	}
	return db.Close()
}
func audit(tx *gorm.DB, kind, job, doc string, details map[string]any) error {
	return tx.Create(&Event{ID: id(), Kind: kind, JobID: job, DocumentID: doc, Details: details}).Error
}

var ErrConflict = errors.New("job is committed; create another job to add sources")

func lockJob(tx *gorm.DB, jobID string) (Job, error) {
	var j Job
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&j, "id = ?", jobID).Error
	if err == nil && j.Status != "review" {
		err = ErrConflict
	}
	return j, err
}
func (s *Store) CreateJob(ctx context.Context, name string, categories []string) (Job, error) {
	j := Job{ID: id(), Name: name, Status: "review", Categories: categories}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateCategories(tx, categories); err != nil {
			return err
		}
		if err := tx.Create(&j).Error; err != nil {
			return err
		}
		return audit(tx, "job.created", j.ID, "", map[string]any{"name": name})
	})
	return j, err
}
func validateCategories(tx *gorm.DB, ids []string) error {
	seen := map[string]bool{}
	for _, cat := range ids {
		if seen[cat] {
			return fmt.Errorf("duplicate category %s", cat)
		}
		seen[cat] = true
		var n int64
		if err := tx.Model(&Category{}).Where("id = ?", cat).Count(&n).Error; err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("unknown category %s", cat)
		}
	}
	return nil
}

type JobView struct {
	Job         Job          `json:"job"`
	Documents   []Document   `json:"documents"`
	Receipts    []Receipt    `json:"receipts"`
	Proposals   []Proposal   `json:"proposals"`
	Duplicates  []Duplicate  `json:"duplicates"`
	Memberships []Membership `json:"memberships"`
	Tasks       []Task       `json:"tasks"`
}

func (s *Store) View(ctx context.Context, jobID string) (JobView, error) {
	v := JobView{Documents: []Document{}, Receipts: []Receipt{}, Proposals: []Proposal{}, Duplicates: []Duplicate{}, Memberships: []Membership{}, Tasks: []Task{}}
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&v.Job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if err := tx.Where("job_id = ?", jobID).Order("created_at").Find(&v.Receipts).Error; err != nil {
			return err
		}
		sub := tx.Model(&Receipt{}).Select("document_id").Where("job_id = ? AND document_id <> ''", jobID)
		if err := tx.Where("id IN (?)", sub).Order("created_at").Find(&v.Documents).Error; err != nil {
			return err
		}
		if err := tx.Where("job_id = ?", jobID).Order("created_at").Find(&v.Proposals).Error; err != nil {
			return err
		}
		if err := tx.Where("document_id IN (?) OR candidate_id IN (?)", sub, sub).Find(&v.Duplicates).Error; err != nil {
			return err
		}
		if err := tx.Where("document_id IN (?)", sub).Find(&v.Memberships).Error; err != nil {
			return err
		}
		return tx.Where("document_id IN (?)", sub).Find(&v.Tasks).Error
	})
	return v, err
}

// IndexOne holds a row lock while indexing. A crash rolls back the task; retries
// use a stable remote-file URL, so a partial cross-service write is idempotent.
func (s *Store) IndexOne(ctx context.Context, index func(context.Context, Document, string) error) (bool, error) {
	worked := false
	err := s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var task Task
		err := tx.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).Where("status IN ? AND next_attempt <= ?", []string{"pending", "retry"}, time.Now()).Order("next_attempt").First(&task).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		worked = true
		var doc Document
		if err = tx.First(&doc, "id = ?", task.DocumentID).Error; err != nil {
			return err
		}
		content, readErr := os.ReadFile(doc.TextPath)
		if readErr == nil {
			readErr = index(ctx, doc, string(content))
		}
		task.Attempts++
		if readErr != nil {
			task.Error = readErr.Error()
			task.Status = "retry"
			task.NextAttempt = time.Now().Add(time.Duration(min(task.Attempts*5, 300)) * time.Second)
			doc.Status = "index_failed"
		} else {
			task.Error = ""
			task.Status = "done"
			doc.Status = "indexed"
		}
		if err = tx.Save(&task).Error; err != nil {
			return err
		}
		if err = tx.Save(&doc).Error; err != nil {
			return err
		}
		return audit(tx, "index."+task.Status, "", doc.ID, map[string]any{"attempt": task.Attempts, "error": task.Error})
	})
	return worked, err
}
