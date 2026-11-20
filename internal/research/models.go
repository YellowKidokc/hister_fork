// Package research owns durable research workflows; Hister remains the text index.
package research

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

type Job struct {
	ID         string    `json:"id" gorm:"primaryKey"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	Categories []string  `json:"categories" gorm:"serializer:json;type:jsonb"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (Job) TableName() string { return "tp_jobs" }

type Document struct {
	ID              string    `json:"id" gorm:"primaryKey"`
	SHA256          string    `json:"sha256" gorm:"uniqueIndex;not null"`
	Name            string    `json:"name"`
	Title           string    `json:"title"`
	Size            int64     `json:"size"`
	Path            string    `json:"canonical_path"`
	TextPath        string    `json:"-"`
	TextHash        string    `json:"text_hash" gorm:"index"`
	Fingerprints    []string  `json:"-" gorm:"serializer:json;type:jsonb"`
	Status          string    `json:"status" gorm:"index"`
	ExtractionError string    `json:"extraction_error"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (Document) TableName() string { return "tp_documents" }

type Receipt struct {
	ID             string    `json:"id" gorm:"primaryKey"`
	JobID          string    `json:"job_id" gorm:"index;not null"`
	DocumentID     string    `json:"document_id" gorm:"index"`
	Source         string    `json:"source"`
	SHA256         string    `json:"sha256"`
	Kind           string    `json:"kind"`
	ExactDuplicate bool      `json:"exact_duplicate"`
	Error          string    `json:"error"`
	CreatedAt      time.Time `json:"created_at"`
}

func (Receipt) TableName() string { return "tp_receipts" }

type Category struct {
	ID          string `json:"id" gorm:"primaryKey"`
	Name        string `json:"name"`
	Axis        string `json:"axis"`
	AutoAllowed bool   `json:"auto_allowed"`
}

func (Category) TableName() string { return "tp_categories" }

type Membership struct {
	DocumentID     string `json:"document_id" gorm:"primaryKey"`
	CategoryID     string `json:"category_id" gorm:"primaryKey"`
	ReviewRequired bool   `json:"review_required"`
	Source         string `json:"source"`
}

func (Membership) TableName() string { return "tp_memberships" }

type Proposal struct {
	ID         string    `json:"id" gorm:"primaryKey"`
	JobID      string    `json:"job_id" gorm:"index"`
	DocumentID string    `json:"document_id" gorm:"index"`
	CategoryID string    `json:"category_id"`
	Confidence float64   `json:"confidence"`
	Reason     string    `json:"reason"`
	Source     string    `json:"source"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
}

func (Proposal) TableName() string { return "tp_proposals" }

type Duplicate struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	DocumentID  string    `json:"document_id" gorm:"uniqueIndex:tp_duplicate_pair"`
	CandidateID string    `json:"candidate_id" gorm:"uniqueIndex:tp_duplicate_pair"`
	Similarity  float64   `json:"similarity"`
	Kind        string    `json:"kind"`
	CreatedAt   time.Time `json:"created_at"`
}

func (Duplicate) TableName() string { return "tp_duplicates" }

type Task struct {
	DocumentID  string    `json:"document_id" gorm:"primaryKey"`
	Status      string    `json:"status" gorm:"index"`
	Attempts    int       `json:"attempts"`
	Error       string    `json:"error"`
	NextAttempt time.Time `json:"next_attempt" gorm:"index"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Task) TableName() string { return "tp_index_tasks" }

type Event struct {
	ID         string         `json:"id" gorm:"primaryKey"`
	Kind       string         `json:"kind" gorm:"index"`
	JobID      string         `json:"job_id" gorm:"index"`
	DocumentID string         `json:"document_id" gorm:"index"`
	Details    map[string]any `json:"details" gorm:"serializer:json;type:jsonb"`
	CreatedAt  time.Time      `json:"created_at"`
}

func (Event) TableName() string { return "tp_events" }

type Export struct {
	ID          string    `json:"id" gorm:"primaryKey"`
	DocumentIDs []string  `json:"document_ids" gorm:"serializer:json;type:jsonb"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
}

func (Export) TableName() string { return "tp_exports" }

func id() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

var DefaultCategories = []Category{
	{"master-equation", "Master Equation", "framework", true},
	{"master-equation-namer", "Master Equation Namer", "framework", true},
	{"lossless-summary", "Lossless Summary", "document-role", true},
	{"axioms", "Axioms", "document-role", true},
	{"theophysics", "Theophysics", "subject", true},
	{"theology", "Theology", "subject", true},
	{"physics", "Physics", "subject", true},
	{"information", "Information", "subject", true},
	{"consciousness", "Consciousness", "subject", true},
	{"moral-order", "Moral Order", "subject", true},
	{"grace-salvation", "Grace and salvation", "subject", true},
	{"series", "Series membership", "series", false},
	{"road", "Road membership", "road", false},
	{"stories", "Stories", "document-role", true},
	{"evidence", "Evidence", "document-role", true},
	{"objections", "Objections", "document-role", true},
	{"formalization", "Formalization", "document-role", true},
	{"canon", "Canon status", "canon", false},
	{"archive", "Archive", "workflow", false},
	{"inbox", "Inbox", "workflow", true},
	{"workbench", "Workbench", "workflow", true},
}
