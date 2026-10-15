package costqueryrealtime

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

const (
	shareTTL          = 2 * time.Hour
	shareScope        = "cost-query-realtime"
	shareTokenBytes   = 32
)

// ShareLink is a reusable share grant bound to one done job.
// Only TokenHash is stored; the raw hashcode is shown once to the admin.
type ShareLink struct {
	ID        string
	TokenHash string
	JobID     string
	Email     string
	Scope     string
	ExpiresAt time.Time
	CreatedBy int64
	CreatedAt time.Time
	Revoked   bool
}

func hashShareToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func generateShareToken() (raw string, err error) {
	buf := make([]byte, shareTokenBytes)
	if _, err = rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (r *Registry) createShare(job *Job, createdBy int64) (rawToken string, link *ShareLink, err error) {
	if job == nil {
		return "", nil, fmt.Errorf("job required")
	}
	job.mu.Lock()
	status := job.Status
	jobID := job.ID
	email := job.Email
	job.mu.Unlock()
	if status != StatusDone {
		return "", nil, fmt.Errorf("job not ready")
	}

	raw, err := generateShareToken()
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	link = &ShareLink{
		ID:        fmt.Sprintf("shr_%d", now.UnixNano()),
		TokenHash: hashShareToken(raw),
		JobID:     jobID,
		Email:     email,
		Scope:     shareScope,
		ExpiresAt: now.Add(shareTTL),
		CreatedBy: createdBy,
		CreatedAt: now,
		Revoked:   false,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.shares == nil {
		r.shares = make(map[string]*ShareLink)
	}
	r.shares[link.TokenHash] = link
	return raw, link, nil
}

// resolveShare returns the share + job for a raw hashcode, or (nil,nil,reason).
// reason is "not_found" | "expired" | "revoked" | "job_missing".
func (r *Registry) resolveShare(rawHashcode string) (*ShareLink, *Job, string) {
	rawHashcode = strings.TrimSpace(rawHashcode)
	if rawHashcode == "" {
		return nil, nil, "not_found"
	}
	hash := hashShareToken(rawHashcode)

	r.mu.Lock()
	link := r.shares[hash]
	r.mu.Unlock()
	if link == nil {
		return nil, nil, "not_found"
	}
	if link.Revoked {
		return nil, nil, "revoked"
	}
	if time.Now().After(link.ExpiresAt) {
		return nil, nil, "expired"
	}
	job := r.get(link.JobID)
	if job == nil {
		return nil, nil, "job_missing"
	}
	job.mu.Lock()
	status := job.Status
	job.mu.Unlock()
	if status != StatusDone {
		return nil, nil, "job_missing"
	}
	return link, job, ""
}

func (r *Registry) latestShareExpiryForJob(jobID string) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest time.Time
	now := time.Now()
	for _, s := range r.shares {
		if s == nil || s.JobID != jobID || s.Revoked {
			continue
		}
		if now.After(s.ExpiresAt) {
			continue
		}
		if s.ExpiresAt.After(latest) {
			latest = s.ExpiresAt
		}
	}
	return latest
}

func (r *Registry) cleanupSharesLocked(now time.Time) {
	for hash, s := range r.shares {
		if s == nil || s.Revoked || now.After(s.ExpiresAt.Add(time.Hour)) {
			delete(r.shares, hash)
		}
	}
}


