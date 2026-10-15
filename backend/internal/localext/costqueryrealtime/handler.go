package costqueryrealtime

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type Handler struct {
	reg *Registry
}

type createJobReq struct {
	Email     string `json:"email"`
	StartDate string `json:"start"`
	EndDate   string `json:"end"`
	Timezone  string `json:"timezone"`
}

func (h *Handler) CreateJob(c *gin.Context) {
	var req createJobReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid body")
		return
	}
	email := strings.TrimSpace(req.Email)
	startDate := strings.TrimSpace(req.StartDate)
	endDate := strings.TrimSpace(req.EndDate)
	tz := strings.TrimSpace(req.Timezone)
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	if email == "" || startDate == "" || endDate == "" {
		response.BadRequest(c, "email, start, end are required")
		return
	}

	startTime, err := timezone.ParseInUserLocation("2006-01-02", startDate, tz)
	if err != nil {
		response.BadRequest(c, "Invalid start format, use YYYY-MM-DD")
		return
	}
	endTime, err := timezone.ParseInUserLocation("2006-01-02", endDate, tz)
	if err != nil {
		response.BadRequest(c, "Invalid end format, use YYYY-MM-DD")
		return
	}
	// half-open [start, end+1day)
	endTime = endTime.AddDate(0, 0, 1)
	if !endTime.After(startTime) {
		response.BadRequest(c, "end must be >= start")
		return
	}

	if !h.reg.tryAcquireSlot() {
		response.Error(c, http.StatusTooManyRequests, "已有查询任务在运行，请稍后再试（最多同时 2 个）")
		return
	}

	pg, err := postgresDB()
	if err != nil {
		h.reg.releaseSlot()
		response.Error(c, http.StatusInternalServerError, "数据库连接失败: "+err.Error())
		return
	}
	userID, gotEmail, err := findUserIDByEmail(c.Request.Context(), pg, email)
	if err != nil {
		h.reg.releaseSlot()
		response.BadRequest(c, err.Error())
		return
	}

	var createdBy int64
	if subject, ok := middleware.GetAuthSubjectFromContext(c); ok {
		createdBy = subject.UserID
	}

	id := uuid.NewString()
	job := &Job{
		ID:         id,
		Status:     StatusPending,
		Email:      gotEmail,
		UserID:     userID,
		StartDate:  startDate,
		EndDate:    endDate,
		Timezone:   tz,
		StartTime:  startTime,
		EndTime:    endTime,
		CreatedBy:  createdBy,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		SQLitePath: filepath.Join(h.reg.dataDir, id+".sqlite"),
	}
	h.reg.put(job)
	go h.reg.runJob(job)
	response.Accepted(c, gin.H{"job_id": id})
}

func (h *Handler) GetJob(c *gin.Context) {
	job := h.reg.get(c.Param("id"))
	if job == nil {
		response.Error(c, http.StatusNotFound, "job not found")
		return
	}
	response.Success(c, job.snapshot())
}

func (h *Handler) GetStats(c *gin.Context) {
	job := h.requireDoneJob(c)
	if job == nil {
		return
	}
	db, err := openJobSQLite(job.SQLitePath, true)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	var cnt, modelCount int64
	var sumActual, sumTotal, sumIn, sumOut float64
	err = db.QueryRow(`SELECT cnt, sum_actual, sum_total, sum_in, sum_out, model_count FROM overall_stats`).
		Scan(&cnt, &sumActual, &sumTotal, &sumIn, &sumOut, &modelCount)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "读取统计失败: "+err.Error())
		return
	}
	mrows, err := db.Query(`SELECT model, cnt, sum_actual, sum_total, sum_in, sum_out FROM model_stats ORDER BY sum_actual DESC, cnt DESC`)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer mrows.Close()
	models := make([]gin.H, 0, 64)
	for mrows.Next() {
		var model string
		var mcnt int64
		var sa, st, si, so float64
		if err := mrows.Scan(&model, &mcnt, &sa, &st, &si, &so); err != nil {
			continue
		}
		models = append(models, gin.H{
			"model": model, "count": mcnt,
			"actual_cost": sa, "total_cost": st,
			"input_tokens": si, "output_tokens": so,
		})
	}
	response.Success(c, gin.H{
		"total_calls":         cnt,
		"total_actual_cost":   sumActual,
		"total_cost":          sumTotal,
		"total_input_tokens":  sumIn,
		"total_output_tokens": sumOut,
		"model_count":         modelCount,
		"models":              models,
		"email":               job.Email,
		"start_date":          job.StartDate,
		"end_date":            job.EndDate,
	})
}

func (h *Handler) GetRows(c *gin.Context) {
	job := h.requireDoneJob(c)
	if job == nil {
		return
	}
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	model := c.Query("model")
	q := c.Query("q")
	sort := c.DefaultQuery("sort", "created_at")
	order := strings.ToLower(c.DefaultQuery("order", "desc"))
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	allowedSort := false
	for _, col := range ColumnOrder {
		if col == sort {
			allowedSort = true
			break
		}
	}
	if !allowedSort {
		sort = "created_at"
	}

	db, err := openJobSQLite(job.SQLitePath, true)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer db.Close()

	wheresql, args := buildWhere(model, q)
	var total int64
	if err := db.QueryRow(`SELECT COUNT(*) FROM usage`+wheresql, args...).Scan(&total); err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	dataSQL := fmt.Sprintf(`SELECT * FROM usage%s ORDER BY "%s" %s LIMIT ? OFFSET ?`, wheresql, sort, order)
	args2 := append(append([]any{}, args...), limit, offset)
	rows, err := db.Query(dataSQL, args2...)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	outRows := make([]map[string]any, 0, limit)
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		m := make(map[string]any, len(cols))
		for i, col := range cols {
			v := derefSQL(raw[i])
			if boolCols[col] {
				switch x := v.(type) {
				case int64:
					m[col] = x == 1
					continue
				case int:
					m[col] = x == 1
					continue
				}
			}
			m[col] = v
		}
		outRows = append(outRows, m)
	}
	response.Success(c, gin.H{
		"total":   total,
		"offset":  offset,
		"limit":   limit,
		"columns": ExportColumnOrder,
		"labels":  LabelMap,
		"rows":    outRows,
	})
}

func (h *Handler) CreateExportTicket(c *gin.Context) {
	job := h.requireDoneJob(c)
	if job == nil {
		return
	}
	var uid int64
	if subject, ok := middleware.GetAuthSubjectFromContext(c); ok {
		uid = subject.UserID
	}
	model := c.Query("model")
	q := c.Query("q")
	ticket := h.reg.createTicket(job.ID, model, q, uid)
	response.Success(c, gin.H{
		"ticket": ticket,
		"url":    fmt.Sprintf("/api/v1/admin/cost-query-realtime/export-download?ticket=%s", ticket),
	})
}

func (h *Handler) ExportDownload(c *gin.Context) {
	ticketID := strings.TrimSpace(c.Query("ticket"))
	if ticketID == "" {
		response.BadRequest(c, "missing ticket")
		return
	}
	tk := h.reg.takeTicket(ticketID)
	if tk == nil {
		response.Error(c, http.StatusUnauthorized, "ticket invalid or expired")
		return
	}
	job := h.reg.get(tk.JobID)
	if job == nil {
		response.Error(c, http.StatusNotFound, "job not found")
		return
	}
	job.mu.Lock()
	status := job.Status
	path := job.SQLitePath
	job.mu.Unlock()
	if status != StatusDone {
		response.Error(c, http.StatusConflict, "job not ready")
		return
	}
	streamExportCSV(c, path, tk.Model, tk.Q)
}

func (h *Handler) ExportXLSX(c *gin.Context) {
	// Authenticated streaming export (CSV body; path kept as export.xlsx per design).
	// Prefer CreateExportTicket + ExportDownload for browser download-bar growth.
	job := h.requireDoneJob(c)
	if job == nil {
		return
	}
	streamExportCSV(c, job.SQLitePath, c.Query("model"), c.Query("q"))
}

func (h *Handler) requireDoneJob(c *gin.Context) *Job {
	job := h.reg.get(c.Param("id"))
	if job == nil {
		response.Error(c, http.StatusNotFound, "job not found")
		return nil
	}
	job.mu.Lock()
	status := job.Status
	job.mu.Unlock()
	if status != StatusDone {
		response.Error(c, http.StatusConflict, "job not ready: "+string(status))
		return nil
	}
	return job
}

