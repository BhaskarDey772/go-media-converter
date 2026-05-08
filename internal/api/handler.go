package api

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"go-media-converter/internal/job"
)

// Enqueuer allows the handler to submit jobs without depending on the concrete pool type.
type Enqueuer interface {
	Enqueue(jobID string)
}

type Handler struct {
	store     job.Store
	pool      Enqueuer
	outputDir string
	uploadDir string
}

func NewHandler(store job.Store, pool Enqueuer, outputDir, uploadDir string) *Handler {
	return &Handler{store: store, pool: pool, outputDir: outputDir, uploadDir: uploadDir}
}

// Upload godoc
// @Summary      Upload MP4 for HLS conversion
// @Description  Accepts a multipart MP4 upload, validates MIME type, creates an async conversion job, and returns the job ID.
// @Tags         jobs
// @Accept       multipart/form-data
// @Produce      json
// @Param        file  formData  file  true  "MP4 video file"
// @Success      202   {object}  map[string]string
// @Failure      400   {object}  map[string]string
// @Failure      413   {object}  map[string]string
// @Failure      500   {object}  map[string]string
// @Router       /api/v1/upload [post]
func (h *Handler) Upload(c *gin.Context) {
	fh, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing file field", "code": "MISSING_FILE"})
		return
	}

	f, err := fh.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open file", "code": "INTERNAL_ERROR"})
		return
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	mime := http.DetectContentType(buf[:n])
	if mime != "video/mp4" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file must be video/mp4", "code": "INVALID_MIME"})
		return
	}

	jobID := uuid.New().String()
	inputPath := filepath.Join(h.uploadDir, jobID+".mp4")
	outputDir := filepath.Join(h.outputDir, jobID)

	if err := os.MkdirAll(h.uploadDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot create upload dir", "code": "INTERNAL_ERROR"})
		return
	}
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot create output dir", "code": "INTERNAL_ERROR"})
		return
	}
	if err := c.SaveUploadedFile(fh, inputPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot save file", "code": "INTERNAL_ERROR"})
		return
	}

	j := job.Job{
		ID:        jobID,
		Status:    job.StatusPending,
		InputPath: inputPath,
		OutputDir: outputDir,
		CreatedAt: time.Now(),
	}
	if err := h.store.Create(j); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot create job", "code": "INTERNAL_ERROR"})
		return
	}

	h.pool.Enqueue(jobID)
	slog.Info("job enqueued", "job_id", jobID)
	c.JSON(http.StatusAccepted, gin.H{"job_id": jobID})
}

// GetJob godoc
// @Summary      Get job status
// @Description  Returns current status. When done, hls_url is populated.
// @Tags         jobs
// @Produce      json
// @Param        id   path      string  true  "Job ID"
// @Success      200  {object}  job.Job
// @Failure      404  {object}  map[string]string
// @Router       /api/v1/jobs/{id} [get]
func (h *Handler) GetJob(c *gin.Context) {
	id := c.Param("id")
	j, err := h.store.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found", "code": "JOB_NOT_FOUND"})
		return
	}
	c.JSON(http.StatusOK, j)
}

// ServeHLS godoc
// @Summary      Serve HLS files
// @Description  Serves master.m3u8, rendition playlists, and .ts segments for a completed job.
// @Tags         hls
// @Param        job_id    path  string  true  "Job ID"
// @Param        filepath  path  string  true  "File path (master.m3u8, 360p.m3u8, segments/360p_000.ts, etc.)"
// @Success      200
// @Failure      404  {object}  map[string]string
// @Failure      409  {object}  map[string]string
// @Router       /hls/{job_id}/{filepath} [get]
func (h *Handler) ServeHLS(c *gin.Context) {
	jobID := c.Param("job_id")
	filePath := c.Param("filepath") // starts with "/"

	j, err := h.store.Get(jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "job not found", "code": "JOB_NOT_FOUND"})
		return
	}
	if j.Status != job.StatusDone {
		c.JSON(http.StatusConflict, gin.H{"error": "job not complete", "code": "JOB_NOT_READY"})
		return
	}

	fullPath := filepath.Join(h.outputDir, jobID, filePath)
	if filepath.Ext(filePath) == ".m3u8" {
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
	}
	slog.Info("file served", "job_id", jobID, "file", filePath)
	c.File(fullPath)
}

// Health godoc
// @Summary      Health check
// @Tags         system
// @Produce      json
// @Success      200  {object}  map[string]string
// @Router       /health [get]
func (h *Handler) Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
