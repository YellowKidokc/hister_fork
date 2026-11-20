// SPDX-License-Identifier: AGPL-3.0-or-later

package model

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DocumentAnnotation stores private user edits outside the search index so
// reindexing a page does not overwrite them.
type DocumentAnnotation struct {
	UserID     uint     `gorm:"primaryKey;autoIncrement:false" json:"-"`
	DocumentID string   `gorm:"primaryKey" json:"-"`
	Tags       []string `gorm:"serializer:json" json:"tags"`
	Notes      string   `json:"notes"`
	Summary    string   `json:"summary"`
}

func GetDocumentAnnotation(ctx context.Context, userID uint, documentID string) (DocumentAnnotation, error) {
	annotation := DocumentAnnotation{UserID: userID, DocumentID: documentID, Tags: []string{}}
	err := DB.WithContext(ctx).Where("user_id = ? AND document_id = ?", userID, documentID).Take(&annotation).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return annotation, err
}

func SaveDocumentAnnotation(ctx context.Context, annotation *DocumentAnnotation) error {
	return DB.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "document_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"tags", "notes", "summary"}),
	}).Create(annotation).Error
}
