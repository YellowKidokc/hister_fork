// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"context"
	"time"

	"gorm.io/gorm/clause"
)

// VisualArchive is a private, explicitly saved screenshot of an indexed page.
type VisualArchive struct {
	UserID     uint   `gorm:"primaryKey;autoIncrement:false"`
	DocumentID string `gorm:"primaryKey"`
	Image      []byte
	CapturedAt time.Time
}

func SaveVisualArchive(ctx context.Context, archive *VisualArchive) error {
	return DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "document_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"image", "captured_at"}),
	}).Create(archive).Error
}

func ReadVisualArchive(ctx context.Context, userID uint, documentID string) (VisualArchive, error) {
	var archive VisualArchive
	err := DB.WithContext(ctx).Take(&archive, "user_id = ? AND document_id = ?", userID, documentID).Error
	return archive, err
}

func DeleteVisualArchive(ctx context.Context, userID uint, documentID string) error {
	return DB.WithContext(ctx).Where("user_id = ? AND document_id = ?", userID, documentID).Delete(&VisualArchive{}).Error
}

// DeleteDocumentVisualArchives removes screenshots for a deleted document.
func DeleteDocumentVisualArchives(documentID string) error {
	if DB == nil {
		return nil
	}
	return DB.Where("document_id = ?", documentID).Delete(&VisualArchive{}).Error
}
