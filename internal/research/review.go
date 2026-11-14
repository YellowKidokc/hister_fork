package research

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ResolveProposal records an explicit human decision after confidence-policy
// commit. It never removes another proposal's or a human's existing membership.
func (s *Store) ResolveProposal(ctx context.Context, proposalID string, accept bool) error {
	return s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var p Proposal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&p, "id = ?", proposalID).Error; err != nil {
			return err
		}
		if p.Status != "held" && p.Status != "review" {
			return errors.New("proposal is not in the review queue")
		}
		if accept {
			m := Membership{DocumentID: p.DocumentID, CategoryID: p.CategoryID, Source: "user", ReviewRequired: false}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "document_id"}, {Name: "category_id"}}, DoUpdates: clause.AssignmentColumns([]string{"source", "review_required"})}).Create(&m).Error; err != nil {
				return err
			}
			p.Status = "accepted"
		} else {
			// A medium-confidence label was provisional; remove only its AI-owned
			// provisional membership, never a prior human-approved classification.
			if p.Status == "review" {
				if err := tx.Where("document_id = ? AND category_id = ? AND source = 'ai' AND review_required = true", p.DocumentID, p.CategoryID).Delete(&Membership{}).Error; err != nil {
					return err
				}
			}
			p.Status = "rejected"
		}
		if err := tx.Save(&p).Error; err != nil {
			return err
		}
		return audit(tx, "classification."+p.Status, p.JobID, p.DocumentID, map[string]any{"proposal_id": p.ID, "category": p.CategoryID, "source": "user"})
	})
}
