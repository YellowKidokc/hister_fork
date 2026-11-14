package research

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Decision struct {
	DocumentID string `json:"document_id"`
	// Explicit selections are human decisions, independent of model confidence.
	Categories []string `json:"categories"`
}

// Commit freezes this job, records human decisions, and transactionally queues
// index writes. No index operation is performed before the PostgreSQL commit.
func (s *Store) Commit(ctx context.Context, jobID string, decisions []Decision) error {
	if len(decisions) == 0 {
		return errors.New("select at least one document")
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := lockJob(tx, jobID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, decision := range decisions {
			if seen[decision.DocumentID] {
				return errors.New("duplicate document decision")
			}
			seen[decision.DocumentID] = true
			var receipt Receipt
			if err = tx.Where("job_id = ? AND document_id = ?", jobID, decision.DocumentID).First(&receipt).Error; err != nil {
				return errors.New("document does not belong to this job")
			}
			var doc Document
			if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&doc, "id = ?", decision.DocumentID).Error; err != nil {
				return err
			}
			if doc.ExtractionError != "" {
				return fmt.Errorf("%s has no extracted text; leave it unselected", doc.Name)
			}
			if err = validateCategories(tx, decision.Categories); err != nil {
				return err
			}
			cats := decision.Categories
			if len(cats) == 0 {
				cats = []string{"inbox", "workbench"}
			}
			// Existing memberships remain: a new source occurrence cannot silently
			// remove another job's category or turn relevance into canonical status.
			for _, cat := range cats {
				m := Membership{DocumentID: doc.ID, CategoryID: cat, Source: "user"}
				if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "document_id"}, {Name: "category_id"}}, DoUpdates: clause.AssignmentColumns([]string{"source", "review_required"})}).Create(&m).Error; err != nil {
					return err
				}
			}
			if err = tx.Model(&Proposal{}).Where("job_id = ? AND document_id = ? AND status = ?", jobID, doc.ID, "suggested").Update("status", "reviewed").Error; err != nil {
				return err
			}
			if doc.Status != "indexed" {
				task := Task{DocumentID: doc.ID, Status: "pending", NextAttempt: time.Now()}
				if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "document_id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "next_attempt"})}).Create(&task).Error; err != nil {
					return err
				}
				if err = tx.Model(&doc).Update("status", "queued").Error; err != nil {
					return err
				}
			}
			if err = audit(tx, "classification.committed", jobID, doc.ID, map[string]any{"categories": cats, "source": "user"}); err != nil {
				return err
			}
		}
		job.Status = "committed"
		if err = tx.Save(&job).Error; err != nil {
			return err
		}
		return audit(tx, "job.committed", jobID, "", map[string]any{"selected": len(decisions)})
	})
}

// Confidence policy is explicit and never grants authority over canon, series,
// roads, archive state, evidence quality, validity, or personal relevance.
func proposalDisposition(c Category, confidence float64) string {
	if !c.AutoAllowed {
		return "held"
	}
	if confidence >= 0.95 {
		return "automatic"
	}
	if confidence >= 0.80 {
		return "review"
	}
	return "held"
}

// ApplyProposals is an explicit alternative to human category selections: a user
// first previews the job, then opts into the conservative confidence policy.
func (s *Store) ApplyProposals(ctx context.Context, jobID string, ids []string) error {
	if len(ids) == 0 {
		return errors.New("select at least one document")
	}
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := lockJob(tx, jobID)
		if err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, docID := range ids {
			if seen[docID] {
				return errors.New("duplicate document")
			}
			seen[docID] = true
			var n int64
			if err = tx.Model(&Receipt{}).Where("job_id = ? AND document_id = ?", jobID, docID).Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				return errors.New("document does not belong to job")
			}
			var doc Document
			if err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&doc, "id = ?", docID).Error; err != nil {
				return err
			}
			if doc.ExtractionError != "" {
				return errors.New("selected document has no extracted text")
			}
			var proposals []Proposal
			if err = tx.Where("job_id = ? AND document_id = ? AND status = 'suggested'", jobID, docID).Find(&proposals).Error; err != nil {
				return err
			}
			applied := 0
			for _, p := range proposals {
				var category Category
				if err = tx.First(&category, "id = ?", p.CategoryID).Error; err != nil {
					return err
				}
				state := proposalDisposition(category, p.Confidence)
				if p.Source == "user" {
					state = "automatic"
				}
				if p.Source == "fallback" {
					state = "fallback"
				}
				p.Status = state
				if state != "held" && state != "fallback" {
					applied++
					m := Membership{DocumentID: docID, CategoryID: p.CategoryID, Source: p.Source, ReviewRequired: state == "review"}
					// Never let a later model downgrade an existing human decision.
					if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&m).Error; err != nil {
						return err
					}
				}
				if err = tx.Save(&p).Error; err != nil {
					return err
				}
				if err = audit(tx, "classification."+state, jobID, docID, map[string]any{"category": p.CategoryID, "confidence": p.Confidence, "source": p.Source}); err != nil {
					return err
				}
			}
			if applied == 0 {
				for _, cat := range []string{"inbox", "workbench"} {
					if err = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Membership{DocumentID: docID, CategoryID: cat, Source: "fallback"}).Error; err != nil {
						return err
					}
				}
			}
			if doc.Status != "indexed" {
				task := Task{DocumentID: docID, Status: "pending", NextAttempt: time.Now()}
				if err = tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "document_id"}}, DoUpdates: clause.AssignmentColumns([]string{"status", "next_attempt"})}).Create(&task).Error; err != nil {
					return err
				}
				if err = tx.Model(&doc).Update("status", "queued").Error; err != nil {
					return err
				}
			}
		}
		job.Status = "committed"
		if err = tx.Save(&job).Error; err != nil {
			return err
		}
		return audit(tx, "job.committed", jobID, "", map[string]any{"selected": len(ids), "policy": "conservative"})
	})
}
