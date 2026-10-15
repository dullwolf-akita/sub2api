package costqueryrealtime

import (
	"log"
	"os"
	"path/filepath"
	"time"

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
	log.Printf("[cost-query-realtime] temp dir=%s ttl=24h share_ttl=%s max_concurrent=%d", dir, shareTTL, maxConcurrent)
	return &Handler{
		reg:            reg,
		shareLimiter:   newIPLimiter(60, time.Minute),
		shareExportLim: newIPLimiter(10, time.Minute),
	}, nil
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
		g.POST("/jobs/:id/share", h.CreateShare)
	}
}

// RegisterPublicRoutes mounts ticket download + public share APIs (no admin JWT).
func (h *Handler) RegisterPublicRoutes(v1 *gin.RouterGroup) {
	v1.GET("/admin/cost-query-realtime/export-download", h.ExportDownload)

	share := v1.Group("/share/cost-query-realtime")
	share.Use(shareNoReferrer(), h.shareLimiter.middleware())
	{
		share.GET("/meta", h.ShareMeta)
		share.GET("/stats", h.ShareStats)
		share.GET("/rows", h.ShareRows)
		share.GET("/export.csv", h.shareExportLim.middleware(), h.ShareExport)
		share.GET("/export.xlsx", h.shareExportLim.middleware(), h.ShareExport)
	}
}
