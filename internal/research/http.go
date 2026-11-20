package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/asciimoo/nogon/client"
	"gorm.io/gorm"
)

//go:embed web/*
var assets embed.FS

type API struct {
	Store           *Store
	Hister          *client.Client
	Token           string
	Classifier      *Classifier
	URLHosts        []string
	SourceClient    *http.Client
	VolumeOneFolder string
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	intakeSlots := make(chan struct{}, 2)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		db, err := a.Store.DB.DB()
		if err != nil || db.PingContext(r.Context()) != nil {
			http.Error(w, "database unavailable", 503)
			return
		}
		jsonResponse(w, 200, map[string]any{"status": "ok"})
	})
	mux.HandleFunc("GET /v1/config", func(w http.ResponseWriter, r *http.Request) {
		var cats []Category
		if err := a.Store.DB.WithContext(r.Context()).Order("axis,name").Find(&cats).Error; err != nil {
			fail(w, err)
			return
		}
		jsonResponse(w, 200, map[string]any{"categories": cats, "ai_enabled": a.Classifier.Enabled(), "url_hosts": a.URLHosts, "max_file_bytes": MaxFileBytes})
	})
	mux.HandleFunc("GET /v1/jobs", a.jobs)
	mux.HandleFunc("POST /v1/jobs", a.createJob)
	mux.HandleFunc("GET /v1/jobs/{id}", a.viewJob)
	mux.HandleFunc("POST /v1/jobs/{id}/files", a.upload)
	mux.HandleFunc("POST /v1/jobs/{id}/url", a.importURL)
	mux.HandleFunc("POST /v1/jobs/{id}/links", a.links)
	mux.HandleFunc("POST /v1/jobs/{id}/classify", a.classify)
	mux.HandleFunc("POST /v1/jobs/{id}/commit", a.commit)
	mux.HandleFunc("GET /v1/documents/{id}", a.document)
	mux.HandleFunc("GET /v1/documents/{id}/source", a.source)
	mux.HandleFunc("GET /v1/search", a.search)
	mux.HandleFunc("POST /v1/export", a.export)
	mux.HandleFunc("GET /v1/events", a.events)
	mux.HandleFunc("GET /v1/storage", a.storageInventory)
	mux.HandleFunc("GET /v1/storage/plans", a.storagePlans)
	mux.HandleFunc("POST /v1/storage/preview", a.previewStorage)
	mux.HandleFunc("GET /v1/review", a.reviewQueue)
	mux.HandleFunc("POST /v1/review/{id}", a.resolveReview)
	static, _ := fs.Sub(assets, "web")
	mux.Handle("GET /", http.FileServer(http.FS(static)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/v1/") {
			w.Header().Set("Cache-Control", "no-store")
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if a.Token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(a.Token)) != 1 {
				jsonResponse(w, 401, map[string]string{"error": "Enter the control center access token."})
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, e := url.Parse(origin)
				if e != nil || !strings.EqualFold(u.Host, r.Host) {
					jsonResponse(w, 403, map[string]string{"error": "Cross-origin requests are not allowed."})
					return
				}
			}
		}

		if r.Method == http.MethodPost && (strings.HasSuffix(r.URL.Path, "/files") || strings.HasSuffix(r.URL.Path, "/url") || strings.HasSuffix(r.URL.Path, "/links") || strings.HasSuffix(r.URL.Path, "/classify")) {
			select {
			case intakeSlots <- struct{}{}:
				defer func() { <-intakeSlots }()
			default:
				w.Header().Set("Retry-After", "2")
				jsonResponse(w, 429, map[string]string{"error": "Intake is busy. Retry shortly."})
				return
			}
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Print("research API panic")
				http.Error(w, "internal server error", 500)
			}
		}()
		mux.ServeHTTP(w, r)
	})
}
func jsonResponse(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func fail(w http.ResponseWriter, err error) {
	status := 400
	message := err.Error()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		status = 404
		message = "Record not found."
	} else if errors.Is(err, ErrConflict) {
		status = 409
	} else {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			status = 413
			message = "Request exceeds the upload limit."
		}
		// SQL/OS internals stay in server logs, never in an HTTP response.
		if strings.Contains(message, "SQLSTATE") || strings.Contains(message, "dial tcp") || strings.Contains(message, "permission denied") {
			log.Printf("research API error: %v", err)
			status = 503
			message = "Storage is unavailable; check service logs."
		}
	}
	jsonResponse(w, status, map[string]string{"error": message})
}
func decode(w http.ResponseWriter, r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return errors.New("invalid JSON request")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}
func (a *API) jobs(w http.ResponseWriter, r *http.Request) {
	jobs := []Job{}
	err := a.Store.DB.WithContext(r.Context()).Order("created_at DESC").Limit(200).Find(&jobs).Error
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 200, jobs)
}
func (a *API) createJob(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string   `json:"name"`
		Categories []string `json:"categories"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" || len(body.Name) > 200 {
		fail(w, errors.New("job name must contain 1–200 bytes"))
		return
	}
	job, err := a.Store.CreateJob(r.Context(), body.Name, body.Categories)
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 201, job)
}
func (a *API) viewJob(w http.ResponseWriter, r *http.Request) {
	view, err := a.Store.View(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 200, view)
}
func filePart(w http.ResponseWriter, r *http.Request) (string, io.ReadCloser, error) {
	r.Body = http.MaxBytesReader(w, r.Body, MaxFileBytes+(1<<20))
	reader, err := r.MultipartReader()
	if err != nil {
		return "", nil, errors.New("expected a multipart file")
	}
	for {
		part, e := reader.NextPart()
		if e != nil {
			return "", nil, errors.New("file is missing")
		}
		if part.FormName() == "file" && part.FileName() != "" {
			return part.FileName(), part, nil
		}
		part.Close()
	}
}
func (a *API) upload(w http.ResponseWriter, r *http.Request) {
	name, part, err := filePart(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	defer part.Close()
	source := r.URL.Query().Get("source")
	if source == "" {
		source = name
	}
	if len(source) > 4000 {
		fail(w, errors.New("source path is too long"))
		return
	}
	receipt, err := a.Store.Ingest(r.Context(), r.PathValue("id"), name, source, "file", part)
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 201, receipt)
}
func (a *API) links(w http.ResponseWriter, r *http.Request) {
	name, part, err := filePart(w, r)
	if err != nil {
		fail(w, err)
		return
	}
	defer part.Close()
	raw, err := io.ReadAll(io.LimitReader(part, MaxFileBytes+1))
	if err != nil || len(raw) > MaxFileBytes {
		fail(w, errors.New("spreadsheet exceeds size limit"))
		return
	}
	links, err := SpreadsheetURLs(name, raw)
	if err != nil {
		fail(w, err)
		return
	}
	receipt, err := a.Store.Ingest(r.Context(), r.PathValue("id"), name, name, "link-spreadsheet", bytes.NewReader(raw))
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"urls": links, "receipt": receipt})
}
func (a *API) importURL(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	jobID := r.PathValue("id")
	var job Job
	if err := a.Store.DB.WithContext(r.Context()).First(&job, "id = ?", jobID).Error; err != nil {
		fail(w, err)
		return
	}
	if job.Status != "review" {
		fail(w, ErrConflict)
		return
	}
	if len(body.URL) > 8192 {
		fail(w, errors.New("URL exceeds 8192 bytes"))
		return
	}
	u, err := ValidateSourceURL(body.URL, a.URLHosts)
	var receipt Receipt
	if err == nil {
		req, e := http.NewRequestWithContext(r.Context(), "GET", u.String(), nil)
		if e != nil {
			err = e
		} else {
			req.Header.Set("User-Agent", "Theophysics-Research/1.0")
			sourceClient := a.SourceClient
			if sourceClient == nil {
				sourceClient = SourceHTTP(a.URLHosts)
			}
			resp, e := sourceClient.Do(req)
			if e != nil {
				err = errors.New("URL download failed; check source access and redirect host allowlist")
			} else {
				defer resp.Body.Close()
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					err = fmt.Errorf("source returned HTTP %d", resp.StatusCode)
				} else {
					name := path.Base(resp.Request.URL.Path)
					if name == "." || name == "/" || name == "" {
						name = "page"
					}
					if strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
						name += ".html"
					}
					receipt, err = a.Store.Ingest(r.Context(), jobID, name, u.String(), "url", resp.Body)
				}
			}
		}
	}
	if err != nil {
		// Every failed fetch has a source receipt too. Validate job ownership/state
		// even when the network operation never started.
		recordErr := a.Store.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
			if _, e := lockJob(tx, jobID); e != nil {
				return e
			}
			rec := Receipt{ID: id(), JobID: jobID, Kind: "url", Source: body.URL, Error: err.Error()}
			if e := tx.Create(&rec).Error; e != nil {
				return e
			}
			return audit(tx, "source.failed", jobID, "", map[string]any{"receipt_id": rec.ID, "source": body.URL, "error": err.Error()})
		})
		if recordErr != nil {
			fail(w, recordErr)
			return
		}
		fail(w, err)
		return
	}
	jsonResponse(w, 201, receipt)
}
func (a *API) classify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		DocumentID string `json:"document_id"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	if err := a.Store.Classify(r.Context(), r.PathValue("id"), body.DocumentID, a.Classifier); err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func (a *API) commit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decisions []Decision `json:"decisions"`
		PolicyIDs []string   `json:"policy_ids"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	if len(body.Decisions) > 0 && len(body.PolicyIDs) > 0 {
		fail(w, errors.New("choose human decisions or confidence policy, not both"))
		return
	}
	var err error
	if len(body.PolicyIDs) > 0 {
		err = a.Store.ApplyProposals(r.Context(), r.PathValue("id"), body.PolicyIDs)
	} else {
		err = a.Store.Commit(r.Context(), r.PathValue("id"), body.Decisions)
	}
	if err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 202, map[string]string{"status": "committed", "index": "queued"})
}
func (a *API) document(w http.ResponseWriter, r *http.Request) {
	var doc Document
	if err := a.Store.DB.WithContext(r.Context()).First(&doc, "id = ?", r.PathValue("id")).Error; err != nil {
		fail(w, err)
		return
	}
	text := ""
	if doc.TextPath != "" {
		b, err := os.ReadFile(doc.TextPath)
		if err != nil {
			fail(w, errors.New("extracted text unavailable"))
			return
		}
		text = string(b)
	}
	var cats []Membership
	var receipts []Receipt
	if err := a.Store.DB.WithContext(r.Context()).Where("document_id = ?", doc.ID).Find(&cats).Error; err != nil {
		fail(w, err)
		return
	}
	if err := a.Store.DB.WithContext(r.Context()).Where("document_id = ?", doc.ID).Find(&receipts).Error; err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 200, map[string]any{"document": doc, "text": text, "memberships": cats, "receipts": receipts})
}
func (a *API) source(w http.ResponseWriter, r *http.Request) {
	var doc Document
	if err := a.Store.DB.WithContext(r.Context()).First(&doc, "id = ?", r.PathValue("id")).Error; err != nil {
		fail(w, err)
		return
	}
	f, err := os.Open(doc.Path)
	if err != nil {
		fail(w, errors.New("source unavailable"))
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	name := filepath.Base(strings.ReplaceAll(doc.Name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." {
		name = "source-" + doc.SHA256
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	http.ServeContent(w, r, "source", doc.CreatedAt, f)
}
func documentURL(docID string) string { return "remote-file://theophysics/documents/" + docID }

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	query := a.Store.DB.WithContext(r.Context()).Model(&Document{}).Where("tp_documents.status = ?", "indexed")
	if cat := q.Get("category"); cat != "" {
		query = query.Where("id IN (?)", a.Store.DB.Model(&Membership{}).Select("document_id").Where("category_id = ?", cat))
	}
	if job := q.Get("job"); job != "" {
		query = query.Where("id IN (?)", a.Store.DB.Model(&Receipt{}).Select("document_id").Where("job_id = ?", job))
	}
	next := ""
	upstreamTotal := uint64(0)
	var order []string
	if text := strings.TrimSpace(q.Get("q")); text != "" {
		result, err := a.Hister.Search(&client.SearchQuery{Text: text, Limit: 100, PageKey: q.Get("page_key")})
		if err != nil {
			jsonResponse(w, 502, map[string]string{"error": "Hister search is unavailable."})
			return
		}
		for _, doc := range result.Documents {
			if strings.HasPrefix(doc.URL, "remote-file://theophysics/documents/") {
				order = append(order, strings.TrimPrefix(doc.URL, "remote-file://theophysics/documents/"))
			}
		}
		query = query.Where("id IN ?", order)
		next = result.PageKey
		upstreamTotal = result.Total
	}
	page, _ := strconv.Atoi(q.Get("page"))
	page = max(0, min(page, 1000000))
	var total int64
	if err := query.Count(&total).Error; err != nil {
		fail(w, err)
		return
	}
	docs := []Document{}
	if err := query.Order("created_at DESC").Limit(100).Offset(page * 100).Find(&docs).Error; err != nil {
		fail(w, err)
		return
	}
	if len(order) > 0 {
		byID := map[string]Document{}
		for _, d := range docs {
			byID[d.ID] = d
		}
		docs = []Document{}
		for _, docID := range order {
			if d, ok := byID[docID]; ok {
				docs = append(docs, d)
			}
		}
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
	jsonResponse(w, 200, map[string]any{"documents": docs, "memberships": memberships, "total": total, "upstream_total": upstreamTotal, "next_page_key": next, "page": page, "has_more": next != "" || int64((page+1)*100) < total})
}
func (a *API) export(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	if len(body.IDs) == 0 || len(body.IDs) > 100 {
		fail(w, errors.New("select 1–100 documents per export"))
		return
	}
	var docs []Document
	if err := a.Store.DB.WithContext(r.Context()).Where("id IN ? AND status IN ?", body.IDs, []string{"indexed", "queued", "index_failed"}).Order("created_at").Find(&docs).Error; err != nil {
		fail(w, err)
		return
	}
	if len(docs) != len(body.IDs) {
		fail(w, errors.New("some documents are missing, staged, or selected twice"))
		return
	}
	var b strings.Builder
	for _, d := range docs {
		text, err := os.ReadFile(d.TextPath)
		if err != nil {
			fail(w, errors.New("export content unavailable"))
			return
		}
		if b.Len()+len(text) > 32<<20 {
			fail(w, errors.New("export exceeds 32 MiB; select fewer documents"))
			return
		}
		var receipts []Receipt
		if err = a.Store.DB.WithContext(r.Context()).Where("document_id = ?", d.ID).Order("created_at").Find(&receipts).Error; err != nil {
			fail(w, err)
			return
		}
		var memberships []Membership
		if err = a.Store.DB.WithContext(r.Context()).Where("document_id = ?", d.ID).Order("category_id").Find(&memberships).Error; err != nil {
			fail(w, err)
			return
		}
		fmt.Fprintf(&b, "# %s\n\nDocument ID: `%s`  \nSHA-256: `%s`  \nCanonical source: `%s`\n\n", strings.ReplaceAll(d.Title, "\n", " "), d.ID, d.SHA256, strings.ReplaceAll(d.Path, "`", ""))
		b.WriteString("Categories (independent labels):\n")
		for _, m := range memberships {
			fmt.Fprintf(&b, "- %s\n", m.CategoryID)
		}
		b.WriteString("\nSource receipts:\n")
		for _, rec := range receipts {
			fmt.Fprintf(&b, "- %s · job `%s` · receipt `%s`\n", strings.ReplaceAll(strings.ReplaceAll(rec.Source, "\n", " "), "\r", " "), rec.JobID, rec.ID)
		}
		b.WriteString("\n---\n\n")
		b.Write(text)
		b.WriteString("\n\n---\n\n")
	}
	content := []byte(b.String())
	hash := sha256.Sum256(content)
	record := Export{ID: id(), DocumentIDs: body.IDs, SHA256: hex.EncodeToString(hash[:])}
	if err := a.Store.DB.WithContext(r.Context()).Transaction(func(tx *gorm.DB) error {
		if e := tx.Create(&record).Error; e != nil {
			return e
		}
		return audit(tx, "export.created", "", "", map[string]any{"export_id": record.ID, "documents": body.IDs, "sha256": record.SHA256})
	}); err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="theophysics-`+record.ID+`.md"`)
	w.Header().Set("X-Export-ID", record.ID)
	_, _ = w.Write(content)
}
func (a *API) events(w http.ResponseWriter, r *http.Request) {
	events := []Event{}
	q := a.Store.DB.WithContext(r.Context()).Order("created_at DESC").Limit(200)
	if job := r.URL.Query().Get("job"); job != "" {
		q = q.Where("job_id = ?", job)
	}
	if err := q.Find(&events).Error; err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 200, events)
}
func (a *API) RunWorker(ctx context.Context) {
	index := func(ctx context.Context, d Document, text string) error {
		return a.Hister.AddDocumentJSONContext(ctx, &client.Document{URL: documentURL(d.ID), Title: d.Title, Text: text, Type: client.RemoteFile, Metadata: map[string]any{"theophysics_id": d.ID, "sha256": d.SHA256}})
	}
	for {
		if ctx.Err() != nil {
			return
		}
		taskCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		worked, err := a.Store.IndexOne(taskCtx, index)
		cancel()
		if err != nil && ctx.Err() == nil {
			log.Printf("index worker: %v", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

func (a *API) reviewQueue(w http.ResponseWriter, r *http.Request) {
	proposals := []Proposal{}
	if err := a.Store.DB.WithContext(r.Context()).Where("status IN ?", []string{"held", "review"}).Order("created_at").Limit(200).Find(&proposals).Error; err != nil {
		fail(w, err)
		return
	}
	ids := []string{}
	for _, p := range proposals {
		ids = append(ids, p.DocumentID)
	}
	docs := []Document{}
	if len(ids) > 0 {
		if err := a.Store.DB.WithContext(r.Context()).Where("id IN ?", ids).Find(&docs).Error; err != nil {
			fail(w, err)
			return
		}
	}
	jsonResponse(w, 200, map[string]any{"proposals": proposals, "documents": docs})
}
func (a *API) resolveReview(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Accept *bool `json:"accept"`
	}
	if err := decode(w, r, &body); err != nil {
		fail(w, err)
		return
	}
	if body.Accept == nil {
		fail(w, errors.New("an explicit accept or reject decision is required"))
		return
	}
	if err := a.Store.ResolveProposal(r.Context(), r.PathValue("id"), *body.Accept); err != nil {
		fail(w, err)
		return
	}
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
