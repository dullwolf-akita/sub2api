package costqueryrealtime

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	jobTTL        = 24 * time.Hour
	maxConcurrent = 2
	maxJobRows    = 2_000_000 // soft safety cap
)

type JobStatus string

const (
	StatusPending JobStatus = "pending"
	StatusRunning JobStatus = "running"
	StatusDone    JobStatus = "done"
	StatusFailed  JobStatus = "failed"
)

type Job struct {
	ID         string    `json:"job_id"`
	Status     JobStatus `json:"status"`
	Email      string    `json:"email"`
	UserID     int64     `json:"user_id"`
	StartDate  string    `json:"start_date"`
	EndDate    string    `json:"end_date"`
	Timezone   string    `json:"timezone"`
	StartTime  time.Time `json:"-"`
	EndTime    time.Time `json:"-"`
	Progress   float64   `json:"progress"` // 0-100
	RowsDone   int64     `json:"rows_done"`
	TotalRows  *int64    `json:"total_rows"`
	Error      string    `json:"error,omitempty"`
	CreatedBy  int64     `json:"created_by"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	SQLitePath string    `json:"-"`

	mu sync.Mutex
}

func (j *Job) snapshot() map[string]any {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := map[string]any{
		"job_id":     j.ID,
		"status":     j.Status,
		"email":      j.Email,
		"user_id":    j.UserID,
		"start_date": j.StartDate,
		"end_date":   j.EndDate,
		"timezone":   j.Timezone,
		"progress":   j.Progress,
		"rows_done":  j.RowsDone,
		"total_rows": j.TotalRows,
		"error":      j.Error,
		"created_by": j.CreatedBy,
		"created_at": j.CreatedAt,
		"updated_at": j.UpdatedAt,
	}
	if j.FinishedAt != nil {
		out["finished_at"] = *j.FinishedAt
	}
	return out
}

func (j *Job) setProgress(rows int64, total *int64, status JobStatus, errMsg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.RowsDone = rows
	j.TotalRows = total
	j.Status = status
	j.Error = errMsg
	j.UpdatedAt = time.Now()
	if total != nil && *total > 0 {
		j.Progress = float64(rows) / float64(*total) * 100
		if j.Progress > 100 {
			j.Progress = 100
		}
	} else if status == StatusDone {
		j.Progress = 100
	}
	if status == StatusDone || status == StatusFailed {
		now := time.Now()
		j.FinishedAt = &now
		if status == StatusDone {
			j.Progress = 100
		}
	}
}

type Registry struct {
	mu       sync.Mutex
	jobs     map[string]*Job
	running  int
	dataDir  string
	tickets  map[string]*exportTicket
	shares   map[string]*ShareLink // tokenHash -> share
}

type exportTicket struct {
	JobID     string
	Model     string
	Q         string
	ExpiresAt time.Time
	CreatedBy int64
}

func NewRegistry(dataDir string) (*Registry, error) {
	if dataDir == "" {
		dataDir = filepath.Join("data", "cost-query-realtime")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	r := &Registry{
		jobs:    make(map[string]*Job),
		dataDir: dataDir,
		tickets: make(map[string]*exportTicket),
		shares:  make(map[string]*ShareLink),
	}
	go r.cleanupLoop()
	return r, nil
}

func (r *Registry) cleanupLoop() {
	t := time.NewTicker(30 * time.Minute)
	defer t.Stop()
	for range t.C {
		r.Cleanup()
	}
}

func (r *Registry) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.cleanupSharesLocked(now)

	// jobID -> latest active share expiry (zero if none)
	shareKeep := make(map[string]time.Time)
	for _, s := range r.shares {
		if s == nil || s.Revoked || now.After(s.ExpiresAt) {
			continue
		}
		if s.ExpiresAt.After(shareKeep[s.JobID]) {
			shareKeep[s.JobID] = s.ExpiresAt
		}
	}

	for id, job := range r.jobs {
		job.mu.Lock()
		age := now.Sub(job.CreatedAt)
		status := job.Status
		path := job.SQLitePath
		job.mu.Unlock()

		// Keep job files alive at least until any active share expires.
		if keepUntil, ok := shareKeep[id]; ok && now.Before(keepUntil) {
			continue
		}

		if age > jobTTL || (status == StatusFailed && age > time.Hour) {
			if path != "" {
				_ = os.Remove(path)
				_ = os.Remove(path + "-wal")
				_ = os.Remove(path + "-shm")
			}
			delete(r.jobs, id)
		}
	}
	for tid, tk := range r.tickets {
		if now.After(tk.ExpiresAt) {
			delete(r.tickets, tid)
		}
	}
	// also sweep orphaned files (skip those still referenced by live jobs/shares)
	livePaths := make(map[string]struct{})
	for _, job := range r.jobs {
		job.mu.Lock()
		if job.SQLitePath != "" {
			livePaths[job.SQLitePath] = struct{}{}
		}
		job.mu.Unlock()
	}
	entries, err := os.ReadDir(r.dataDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		full := filepath.Join(r.dataDir, e.Name())
		base := full
		for _, suf := range []string{"-wal", "-shm"} {
			if len(full) > len(suf) && full[len(full)-len(suf):] == suf {
				base = full[:len(full)-len(suf)]
				break
			}
		}
		if _, ok := livePaths[base]; ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > jobTTL {
			_ = os.Remove(full)
		}
	}
}

func (r *Registry) tryAcquireSlot() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running >= maxConcurrent {
		return false
	}
	r.running++
	return true
}

func (r *Registry) releaseSlot() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running > 0 {
		r.running--
	}
}

func (r *Registry) put(job *Job) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs[job.ID] = job
}

func (r *Registry) get(id string) *Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[id]
}

func (r *Registry) createTicket(jobID, model, q string, createdBy int64) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := fmt.Sprintf("%d_%s", time.Now().UnixNano(), jobID[:8])
	r.tickets[id] = &exportTicket{
		JobID:     jobID,
		Model:     model,
		Q:         q,
		ExpiresAt: time.Now().Add(2 * time.Minute),
		CreatedBy: createdBy,
	}
	return id
}

func (r *Registry) takeTicket(id string) *exportTicket {
	r.mu.Lock()
	defer r.mu.Unlock()
	tk, ok := r.tickets[id]
	if !ok {
		return nil
	}
	delete(r.tickets, id)
	if time.Now().After(tk.ExpiresAt) {
		return nil
	}
	return tk
}

func openJobSQLite(path string, readonly bool) (*sql.DB, error) {
	dsn := path
	if readonly {
		dsn = "file:" + path + "?mode=ro"
	} else {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if !readonly {
		_, _ = db.Exec(`PRAGMA journal_mode=WAL`)
		_, _ = db.Exec(`PRAGMA synchronous=NORMAL`)
		_, _ = db.Exec(`PRAGMA temp_store=MEMORY`)
	}
	return db, nil
}

func metaGet(db *sql.DB, key string) (string, error) {
	var v string
	err := db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v, err
}

func metaSet(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO meta(key,value) VALUES(?,?)`, key, value)
	return err
}

func columnsJSON() string {
	b, _ := json.Marshal(ColumnOrder)
	return string(b)
}
