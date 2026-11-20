// SPDX-License-Identifier: AGPL-3.0-or-later

package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/asciimoo/nogon/config"
	"github.com/asciimoo/nogon/internal/research"
	"github.com/asciimoo/nogon/server/crawler"
	"github.com/asciimoo/nogon/server/document"
	"github.com/asciimoo/nogon/server/extractor"
	"github.com/asciimoo/nogon/server/indexer"

	"github.com/rs/zerolog/log"
)

// Link imports: the user uploads a list of links (pasted text, .txt, .csv,
// .tsv or .xlsx) and the server fetches each page with the configured crawler
// and indexes it under an optional label. Imports run in the background; the
// page polls their progress.

const (
	linkImportMaxBytes   = 16 << 20
	linkImportMaxLinks   = 2000
	linkImportURLTimeout = 90 * time.Second
	linkImportMaxLabel   = 200
	linkImportKeep       = 50 // finished imports remembered per server
)

// Link import item states.
const (
	linkPending = "pending"
	linkIndexed = "indexed"
	linkSkipped = "skipped"
	linkFailed  = "failed"
)

type linkImportItem struct {
	URL    string `json:"url"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type linkImport struct {
	mu       sync.Mutex
	ID       string           `json:"id"`
	Label    string           `json:"label"`
	Started  time.Time        `json:"started"`
	Finished *time.Time       `json:"finished,omitempty"`
	Items    []linkImportItem `json:"items"`
	userID   uint
	cancel   context.CancelFunc
}

type linkImportSummary struct {
	ID       string           `json:"id"`
	Label    string           `json:"label"`
	Started  time.Time        `json:"started"`
	Finished *time.Time       `json:"finished,omitempty"`
	Total    int              `json:"total"`
	Done     int              `json:"done"`
	Indexed  int              `json:"indexed"`
	Skipped  int              `json:"skipped"`
	Failed   int              `json:"failed"`
	Items    []linkImportItem `json:"items,omitempty"`
}

var linkImports = struct {
	sync.Mutex
	byID  map[string]*linkImport
	order []string
}{byID: map[string]*linkImport{}}

func init() {
	Endpoints = append(Endpoints,
		&Endpoint{
			Name:         "Import links",
			Path:         "/api/research/links",
			Method:       POST,
			CSRFRequired: true,
			BodyLimit:    func(*config.Config) int64 { return linkImportMaxBytes + 1<<20 },
			Handler:      serveStartLinkImport,
			Description:  "Fetch and index a list of links in the background. Multipart fields: 'urls' (text, one or more links), 'file' (.txt, .csv, .tsv or .xlsx) and optional 'label'.",
		},
		&Endpoint{
			Name:        "List link imports",
			Path:        "/api/research/links",
			Method:      GET,
			NoStore:     true,
			Handler:     serveLinkImports,
			Description: "List the caller's recent link imports with progress counts",
		},
		&Endpoint{
			Name:        "Get link import",
			Path:        "/api/research/links/{id}",
			Method:      GET,
			NoStore:     true,
			Handler:     serveLinkImport,
			Description: "Progress and per-link results of one link import",
		},
		&Endpoint{
			Name:         "Cancel link import",
			Path:         "/api/research/links/{id}/cancel",
			Method:       POST,
			CSRFRequired: true,
			Handler:      serveCancelLinkImport,
			Description:  "Stop a running link import; links not yet fetched are marked skipped",
		},
	)
}

// parseLinkText pulls every http(s) link out of free text such as a pasted
// list or a .txt file. Links may be separated by newlines, spaces or commas.
func parseLinkText(text string) []string {
	out := []string{}
	for _, field := range strings.FieldsFunc(text, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == ','
	}) {
		field = strings.Trim(field, "\"'<>()[]")
		if u, err := url.Parse(field); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Hostname() != "" {
			out = append(out, field)
		}
	}
	return out
}

// collectImportLinks reads links from the request's text field and file.
func collectImportLinks(r *http.Request) ([]string, error) {
	links := parseLinkText(r.FormValue("urls"))
	file, header, err := r.FormFile("file")
	if err == nil {
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, linkImportMaxBytes+1))
		if err != nil {
			return nil, err
		}
		if len(raw) > linkImportMaxBytes {
			return nil, errors.New("file exceeds 16 MiB")
		}
		switch strings.ToLower(filepath.Ext(header.Filename)) {
		case ".csv", ".tsv", ".xlsx":
			fromSheet, err := research.SpreadsheetURLs(header.Filename, raw)
			if err != nil {
				return nil, err
			}
			links = append(links, fromSheet...)
		default:
			links = append(links, parseLinkText(string(raw))...)
		}
	} else if !errors.Is(err, http.ErrMissingFile) {
		return nil, err
	}
	seen := map[string]bool{}
	unique := make([]string, 0, len(links))
	for _, l := range links {
		if !seen[l] {
			seen[l] = true
			unique = append(unique, l)
		}
	}
	if len(unique) == 0 {
		return nil, errors.New("no http:// or https:// links found")
	}
	if len(unique) > linkImportMaxLinks {
		return nil, fmt.Errorf("too many links: %d (limit %d per import)", len(unique), linkImportMaxLinks)
	}
	return unique, nil
}

func serveStartLinkImport(c *webContext) {
	if err := c.Request.ParseMultipartForm(linkImportMaxBytes); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		http.Error(c.Response, "invalid upload: "+err.Error(), http.StatusBadRequest)
		return
	}
	links, err := collectImportLinks(c.Request)
	if err != nil {
		http.Error(c.Response, err.Error(), http.StatusBadRequest)
		return
	}
	label := strings.TrimSpace(c.Request.FormValue("label"))
	if len(label) > linkImportMaxLabel {
		http.Error(c.Response, "label exceeds 200 characters", http.StatusBadRequest)
		return
	}
	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		serve500(c)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &linkImport{
		ID:      hex.EncodeToString(idBytes),
		Label:   label,
		Started: time.Now(),
		Items:   make([]linkImportItem, len(links)),
		userID:  submittedDocumentUserID(c),
		cancel:  cancel,
	}
	for i, l := range links {
		job.Items[i] = linkImportItem{URL: l, Status: linkPending}
	}
	rememberLinkImport(job)
	go runLinkImport(ctx, job, c.Config, c.Indexer, c.effectiveRules())
	c.JSONStatus(http.StatusAccepted, job.summary(false))
}

func rememberLinkImport(job *linkImport) {
	linkImports.Lock()
	defer linkImports.Unlock()
	linkImports.byID[job.ID] = job
	linkImports.order = append(linkImports.order, job.ID)
	for len(linkImports.order) > linkImportKeep {
		oldest := linkImports.byID[linkImports.order[0]]
		if oldest != nil && oldest.isRunning() {
			break
		}
		delete(linkImports.byID, linkImports.order[0])
		linkImports.order = linkImports.order[1:]
	}
}

func (j *linkImport) isRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.Finished == nil
}

func (j *linkImport) set(i int, status string, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Items[i].Status = status
	if err != nil {
		j.Items[i].Error = err.Error()
	}
}

func (j *linkImport) summary(withItems bool) linkImportSummary {
	j.mu.Lock()
	defer j.mu.Unlock()
	s := linkImportSummary{ID: j.ID, Label: j.Label, Started: j.Started, Finished: j.Finished, Total: len(j.Items)}
	for _, it := range j.Items {
		switch it.Status {
		case linkIndexed:
			s.Indexed++
		case linkSkipped:
			s.Skipped++
		case linkFailed:
			s.Failed++
		}
	}
	s.Done = s.Indexed + s.Skipped + s.Failed
	if withItems {
		s.Items = append([]linkImportItem(nil), j.Items...)
	}
	return s
}

func runLinkImport(ctx context.Context, job *linkImport, cfg *config.Config, idx *indexer.Indexer, rules *config.Rules) {
	defer func() {
		now := time.Now()
		job.mu.Lock()
		job.Finished = &now
		for i := range job.Items {
			if job.Items[i].Status == linkPending {
				job.Items[i].Status = linkSkipped
				job.Items[i].Error = "import stopped"
			}
		}
		job.mu.Unlock()
		job.cancel()
	}()
	cr, err := crawler.New(&cfg.Crawler, nil)
	if err != nil {
		log.Error().Err(err).Msg("link import: failed to create crawler")
		for i := range job.Items {
			job.set(i, linkFailed, fmt.Errorf("crawler unavailable: %w", err))
		}
		return
	}
	defer func() {
		if err := cr.Close(); err != nil {
			log.Warn().Err(err).Msg("link import: crawler close error")
		}
	}()
	for i, item := range job.Items {
		if ctx.Err() != nil {
			return
		}
		status, err := importOneLink(ctx, cr, cfg, idx, rules, item.URL, job.Label, job.userID)
		job.set(i, status, err)
	}
	s := job.summary(false)
	log.Info().Str("import", job.ID).Int("indexed", s.Indexed).Int("skipped", s.Skipped).Int("failed", s.Failed).Msg("link import finished")
}

func importOneLink(parent context.Context, cr crawler.Crawler, cfg *config.Config, idx *indexer.Indexer, rules *config.Rules, rawURL, label string, userID uint) (string, error) {
	if cfg.IsSameHost(rawURL) {
		return linkSkipped, errors.New("link points at this Nogon server")
	}
	if rules.IsSkip(rawURL) {
		return linkSkipped, errors.New("blocked by your skip rules")
	}
	if idx.GetByURLAndUser(rawURL, userID) != nil {
		return linkSkipped, errors.New("already indexed")
	}
	ctx, cancel := context.WithTimeout(parent, linkImportURLTimeout)
	defer cancel()
	v, err := crawler.NewValidator(&crawler.ValidatorRules{MaxLinks: 1})
	if err != nil {
		return linkFailed, err
	}
	ch, err := cr.Crawl(ctx, rawURL, v)
	if err != nil {
		return linkFailed, fmt.Errorf("fetch failed: %w", err)
	}
	d, ok := <-ch
	for range ch { // let the single-page crawl finish before reusing the crawler
	}
	if !ok || d == nil {
		if ctx.Err() != nil {
			return linkFailed, errors.New("timed out")
		}
		return linkFailed, errors.New("no response from page")
	}
	if err := d.ProcessContext(ctx, nil, extractor.ExtractContext); err != nil {
		return linkFailed, fmt.Errorf("could not read page: %w", err)
	}
	d.Label = label
	d.UserID = userID
	d.HTMLKey, d.FaviconKey = "", ""
	if err := idx.AddContext(ctx, d, indexer.WithRules(rules)); err != nil {
		if errors.Is(err, document.ErrSensitiveContent) {
			return linkSkipped, errors.New("rejected: sensitive content")
		}
		return linkFailed, fmt.Errorf("indexing failed: %w", err)
	}
	return linkIndexed, nil
}

func linkImportForCaller(c *webContext) (*linkImport, bool) {
	linkImports.Lock()
	job := linkImports.byID[c.Request.PathValue("id")]
	linkImports.Unlock()
	if job == nil || job.userID != submittedDocumentUserID(c) {
		http.Error(c.Response, "import not found", http.StatusNotFound)
		return nil, false
	}
	return job, true
}

func serveLinkImports(c *webContext) {
	uid := submittedDocumentUserID(c)
	linkImports.Lock()
	jobs := make([]*linkImport, 0, len(linkImports.order))
	for i := len(linkImports.order) - 1; i >= 0; i-- {
		if j := linkImports.byID[linkImports.order[i]]; j != nil && j.userID == uid {
			jobs = append(jobs, j)
		}
	}
	linkImports.Unlock()
	out := make([]linkImportSummary, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, j.summary(false))
	}
	c.JSONStatus(http.StatusOK, map[string]any{"imports": out})
}

func serveLinkImport(c *webContext) {
	if job, ok := linkImportForCaller(c); ok {
		c.JSONStatus(http.StatusOK, job.summary(true))
	}
}

func serveCancelLinkImport(c *webContext) {
	if job, ok := linkImportForCaller(c); ok {
		job.cancel()
		c.JSONStatus(http.StatusOK, job.summary(false))
	}
}
