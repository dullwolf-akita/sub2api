package costqueryrealtime

import (
	"log"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// NewHandler creates the localext handler (temp dir under data/cost-query-realtime).
func NewHandler() (*Handler, error) {
	dir := os.Getenv("COST_QUERY_REALTIME_DIR")
	if dir == "" {
		dir = filepath.Join("data", "cost-query-realtime")
	}
	reg, err := NewRegistry(dir)
	if err != nil {
		return nil, err
	}
	log.Printf("[cost-query-realtime] temp dir=%s ttl=24h max_concurrent=%d", dir, maxConcurrent)
	return &Handler{reg: reg}, nil
}

// RegisterAdminRoutes mounts APIs that require admin auth (parent already applied AdminAuth).
func (h *Handler) RegisterAdminRoutes(admin *gin.RouterGroup) {
	g := admin.Group("/cost-query-realtime")
	{
		g.POST("/jobs", h.CreateJob)
		g.GET("/jobs/:id", h.GetJob)
		g.GET("/jobs/:id/stats", h.GetStats)
		g.GET("/jobs/:id/rows", h.GetRows)
		g.GET("/jobs/:id/export.xlsx", h.ExportXLSX)
		g.GET("/jobs/:id/export.csv", h.ExportXLSX)
		g.POST("/jobs/:id/export-ticket", h.CreateExportTicket)
	}
}

// RegisterPublicRoutes mounts the one-time ticket download URL (no admin JWT).
// Path stays under /api/v1/admin/... for consistency; register on v1 *before* or
// *outside* the admin-auth group so the ticket alone authorizes the stream.
func (h *Handler) RegisterPublicRoutes(v1 *gin.RouterGroup) {
	v1.GET("/admin/cost-query-realtime/export-download", h.ExportDownload)
}
