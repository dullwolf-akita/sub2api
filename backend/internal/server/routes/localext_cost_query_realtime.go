package routes

import (
	"log"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/localext/costqueryrealtime"
	"github.com/gin-gonic/gin"
)

// cqrHandler is initialized when admin routes register; public ticket route uses it.
var cqrHandler *costqueryrealtime.Handler

// registerCostQueryRealtimeLocal mounts admin-auth'd cost query realtime APIs.
// LOCAL EXT: keep logic in internal/localext; only this thin hook lives in routes.
func registerCostQueryRealtimeLocal(admin *gin.RouterGroup, _ *handler.Handlers) {
	h, err := costqueryrealtime.NewHandler()
	if err != nil {
		log.Printf("[cost-query-realtime] init failed: %v", err)
		return
	}
	cqrHandler = h
	h.RegisterAdminRoutes(admin)
	log.Printf("[cost-query-realtime] admin routes mounted at /api/v1/admin/cost-query-realtime")
}

// RegisterCostQueryRealtimePublic mounts the ticket download endpoint without admin JWT.
// Call after RegisterAdminRoutes so cqrHandler is ready.
func RegisterCostQueryRealtimePublic(v1 *gin.RouterGroup) {
	if cqrHandler == nil {
		log.Printf("[cost-query-realtime] public routes skipped (handler not init)")
		return
	}
	cqrHandler.RegisterPublicRoutes(v1)
}
