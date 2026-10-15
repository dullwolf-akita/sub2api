package costqueryrealtime

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// CreateShare issues a reusable 2h share link for a done job (admin only).
// POST /api/v1/admin/cost-query-realtime/jobs/:id/share
func (h *Handler) CreateShare(c *gin.Context) {
	job := h.requireDoneJob(c)
	if job == nil {
		return
	}
	var createdBy int64
	if subject, ok := middleware.GetAuthSubjectFromContext(c); ok {
		createdBy = subject.UserID
	}
	raw, link, err := h.reg.createShare(job, createdBy)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	path := fmt.Sprintf("/share/cost-query-realtime?hashcode=%s", raw)
	response.Success(c, gin.H{
		"hashcode":   raw,
		"url":        path,
		"expires_at": link.ExpiresAt,
		"email":      link.Email,
		"job_id":     link.JobID,
		"ttl_seconds": int(shareTTL.Seconds()),
	})
}

func (h *Handler) abortShare(c *gin.Context, reason string) {
	switch reason {
	case "expired":
		c.JSON(http.StatusGone, gin.H{"code": http.StatusGone, "message": "not found"})
	default:
		// invalid / revoked / job_missing → generic 404
		c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "not found"})
	}
	c.Abort()
}

func (h *Handler) shareFromQuery(c *gin.Context) (*ShareLink, *Job, bool) {
	raw := strings.TrimSpace(c.Query("hashcode"))
	if raw == "" {
		raw = strings.TrimSpace(c.GetHeader("X-Share-Hashcode"))
	}
	link, job, reason := h.reg.resolveShare(raw)
	if reason != "" {
		h.abortShare(c, reason)
		return nil, nil, false
	}
	return link, job, true
}

// ShareMeta GET /api/v1/share/cost-query-realtime/meta?hashcode=
func (h *Handler) ShareMeta(c *gin.Context) {
	link, job, ok := h.shareFromQuery(c)
	if !ok {
		return
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	response.Success(c, gin.H{
		"email":      job.Email,
		"start_date": job.StartDate,
		"end_date":   job.EndDate,
		"timezone":   job.Timezone,
		"expires_at": link.ExpiresAt,
		"columns":    ExportColumnOrder,
		"labels":     LabelMap,
	})
}

// ShareStats GET /api/v1/share/cost-query-realtime/stats?hashcode=
func (h *Handler) ShareStats(c *gin.Context) {
	_, job, ok := h.shareFromQuery(c)
	if !ok {
		return
	}
	// reuse admin stats logic via internal path
	c.Params = append(c.Params, gin.Param{Key: "id", Value: job.ID})
	h.GetStats(c)
}

// ShareRows GET /api/v1/share/cost-query-realtime/rows?hashcode=
func (h *Handler) ShareRows(c *gin.Context) {
	_, job, ok := h.shareFromQuery(c)
	if !ok {
		return
	}
	c.Params = append(c.Params, gin.Param{Key: "id", Value: job.ID})
	h.GetRows(c)
}

// ShareExport GET /api/v1/share/cost-query-realtime/export.csv?hashcode=
// Streaming CSV; reusable within share TTL (hashcode authenticates, not single-use).
func (h *Handler) ShareExport(c *gin.Context) {
	_, job, ok := h.shareFromQuery(c)
	if !ok {
		return
	}
	job.mu.Lock()
	path := job.SQLitePath
	status := job.Status
	job.mu.Unlock()
	if status != StatusDone || path == "" {
		h.abortShare(c, "job_missing")
		return
	}
	streamExportCSV(c, path, c.Query("model"), c.Query("q"))
}

