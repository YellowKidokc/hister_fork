package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/PuerkitoBio/goquery"
	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/files/extract"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const MaxFileBytes = 32 << 20

// Ingest streams each source to disk, preserves its exact bytes, and records every
// accepted occurrence, even when an earlier job owns the same content identity.
func (s *Store) Ingest(ctx context.Context, jobID, name, source, kind string, input io.Reader) (Receipt, error) {
	var receipt Receipt
	var job Job
	if err := s.DB.WithContext(ctx).First(&job, "id = ?", jobID).Error; err != nil {
		return receipt, err
	}
	if job.Status != "review" {
		return receipt, ErrConflict
	}
	tmp, err := os.CreateTemp(filepath.Join(s.Root, "incoming"), "upload-")
	if err != nil {
		return receipt, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(input, MaxFileBytes+1))
	if copyErr == nil {
		copyErr = tmp.Sync()
	}
	closeErr := tmp.Close()
	if copyErr != nil {
		return receipt, copyErr
	}
	if closeErr != nil {
		return receipt, closeErr
	}
	if n > MaxFileBytes {
		return receipt, fmt.Errorf("file exceeds %d MiB limit", MaxFileBytes>>20)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	target := filepath.Join(s.Root, "objects", sum)
	// Link is atomic and never replaces an existing canonical object.
	if err = os.Link(tmp.Name(), target); err != nil && !errors.Is(err, os.ErrExist) {
		return receipt, err
	}
	if err = syncDir(filepath.Dir(target)); err != nil {
		return receipt, err
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		return receipt, err
	}
	result, extractErr := extract.New(config.PDFParser{}).Extract(ctx, name, raw)
	if extractErr == nil && result.Text == "" && result.HTML != "" {
		parsed, e := goquery.NewDocumentFromReader(strings.NewReader(result.HTML))
		if e != nil {
			extractErr = e
		} else {
			parsed.Find("script,style,noscript").Remove()
			result.Text = parsed.Find("body").Text()
			if result.Title == "" {
				result.Title = parsed.Find("title").Text()
			}
		}
	}
	if extractErr == nil && strings.TrimSpace(result.Text) == "" {
		extractErr = errors.New("no extractable text; source retained for review")
	}
	title := strings.TrimSpace(result.Title)
	if title == "" {
		title = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	}
	doc := Document{ID: id(), SHA256: sum, Name: name, Title: title, Size: n, Path: target, Status: "staged"}
	if extractErr != nil {
		doc.ExtractionError = extractErr.Error()
	} else {
		doc.TextPath = filepath.Join(s.Root, "text", doc.ID+".txt")
		if err = atomicWrite(doc.TextPath, []byte(result.Text)); err != nil {
			return receipt, err
		}
		doc.TextHash, doc.Fingerprints = fingerprint(result.Text)
	}
	receipt = Receipt{ID: id(), JobID: jobID, Source: source, Kind: kind, SHA256: sum}
	candidateTextPath := doc.TextPath
	err = s.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		j, e := lockJob(tx, jobID)
		if e != nil {
			return e
		}
		inserted := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "sha256"}}, DoNothing: true}).Create(&doc)
		if inserted.Error != nil {
			return inserted.Error
		}
		if inserted.RowsAffected == 0 {
			var existing Document
			if e = tx.First(&existing, "sha256 = ?", sum).Error; e != nil {
				return e
			}
			doc = existing
			receipt.ExactDuplicate = true
		}
		receipt.DocumentID = doc.ID
		if e = tx.Create(&receipt).Error; e != nil {
			return e
		}
		if inserted.RowsAffected > 0 && doc.TextHash != "" {
			var candidates []Document
			// Bounded candidate scan, explicitly advisory; it does not promise exhaustive
			// fuzzy deduplication and never changes canonical identities.
			if e = tx.Where("id <> ? AND text_hash <> ''", doc.ID).Order("created_at DESC").Limit(1000).Find(&candidates).Error; e != nil {
				return e
			}
			for _, candidate := range candidates {
				sim := similarity(doc.Fingerprints, candidate.Fingerprints)
				kind := "probable"
				if doc.TextHash == candidate.TextHash {
					sim = 1
					kind = "normalized-text"
				}
				if sim < 0.85 {
					continue
				}
				d := Duplicate{ID: id(), DocumentID: doc.ID, CandidateID: candidate.ID, Similarity: sim, Kind: kind}
				if e = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&d).Error; e != nil {
					return e
				}
			}
		}
		var count int64
		if e = tx.Model(&Proposal{}).Where("job_id = ? AND document_id = ?", jobID, doc.ID).Count(&count).Error; e != nil {
			return e
		}
		if count == 0 {
			cats := j.Categories
			source := "user"
			confidence := 1.0
			if len(cats) == 0 {
				cats = []string{"inbox", "workbench"}
				source = "fallback"
				confidence = 0
			}
			for _, cat := range cats {
				p := Proposal{ID: id(), JobID: jobID, DocumentID: doc.ID, CategoryID: cat, Confidence: confidence, Source: source, Status: "suggested"}
				if e = tx.Create(&p).Error; e != nil {
					return e
				}
			}
		}
		return audit(tx, "source.received", jobID, doc.ID, map[string]any{"receipt_id": receipt.ID, "sha256": sum, "source": source, "exact_duplicate": receipt.ExactDuplicate, "extraction_error": doc.ExtractionError})
	})
	if err == nil && receipt.ExactDuplicate && candidateTextPath != "" && candidateTextPath != doc.TextPath {
		// This attempt's derivative was never referenced. Never delete on an
		// uncertain database commit, and never touch the existing canonical text.
		_ = os.Remove(candidateTextPath)
	}
	return receipt, err
}
func syncDir(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "text-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Link(f.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	return syncDir(filepath.Dir(path))
}
func fingerprint(text string) (string, []string) {
	// Hash normalized text incrementally. Fingerprint memory stays bounded even
	// for the largest accepted source, rather than retaining millions of shingles.
	h := sha256.New()
	var normalized bytes.Buffer
	hasWord, pendingSpace := false, false
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			if pendingSpace && hasWord {
				normalized.WriteByte(' ')
			}
			normalized.WriteRune(unicode.ToLower(r))
			hasWord = true
			pendingSpace = false
			if normalized.Len() > 65536 {
				_, _ = h.Write(normalized.Bytes())
				normalized.Reset()
			}
		} else {
			pendingSpace = true
		}
	}
	_, _ = h.Write(normalized.Bytes())
	sample := text
	if len(sample) > 256<<10 {
		sample = sample[:256<<10]
		for !utf8.ValidString(sample) {
			sample = sample[:len(sample)-1]
		}
	}
	words := strings.FieldsFunc(strings.ToLower(sample), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	set := map[string]bool{}
	for i := 0; i+4 <= len(words); i++ {
		sum := sha256.Sum256([]byte(strings.Join(words[i:i+4], " ")))
		set[hex.EncodeToString(sum[:8])] = true
	}
	hashes := make([]string, 0, len(set))
	for sum := range set {
		hashes = append(hashes, sum)
	}
	sort.Strings(hashes)
	if len(hashes) > 256 {
		hashes = hashes[:256]
	}
	return hex.EncodeToString(h.Sum(nil)), hashes
}

func similarity(a, b []string) float64 {
	if len(a) < 8 || len(b) < 8 {
		return 0
	}
	set := map[string]bool{}
	for _, v := range a {
		set[v] = true
	}
	overlap := 0
	for _, v := range b {
		if set[v] {
			overlap++
		}
	}
	return float64(overlap) / float64(len(a)+len(b)-overlap)
}
