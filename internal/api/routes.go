package api

import (
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

func RegisterRoutes(r *gin.Engine, h *Handler, maxUploadBytes int64) {
	r.GET("/health", h.Health)
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	v1 := r.Group("/api/v1")
	{
		v1.POST("/upload", ValidateUpload(maxUploadBytes), h.Upload)
		v1.GET("/jobs/:id", h.GetJob)
	}

	r.GET("/hls/:job_id/*filepath", h.ServeHLS)
}
