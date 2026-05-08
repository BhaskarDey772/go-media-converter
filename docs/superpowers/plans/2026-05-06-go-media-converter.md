# Go Media Converter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go HTTP server that accepts MP4 uploads, converts them to adaptive bitrate HLS (360p/720p/1080p), and serves the stream files via async job polling.

**Architecture:** Single binary with a Gin HTTP server and an in-process goroutine worker pool. FFmpeg is invoked via `os/exec`. Job state lives in an in-memory store behind an interface for clean DB swap-in later.

**Tech Stack:** Go 1.21+, Gin, swaggo/swag, google/uuid, FFmpeg (host), log/slog (stdlib)

---

## File Map

| File | Responsibility |
|---|---|
| `cmd/server/main.go` | Entry point: wire config → store → converter → pool → handler → routes → run |
| `internal/config/config.go` | Load env vars into a Config struct |
| `internal/job/types.go` | Job struct and Status enum |
| `internal/job/store.go` | Store interface + MemStore implementation |
| `internal/converter/ffmpeg.go` | Converter interface + FFmpeg exec wrapper + master.m3u8 writer |
| `internal/worker/pool.go` | Goroutine worker pool: queue, dispatch, panic recovery |
| `internal/api/middleware.go` | Upload size validation middleware |
| `internal/api/handler.go` | HTTP handlers: Upload, GetJob, ServeHLS, Health |
| `internal/api/routes.go` | Route registration + Swagger UI mount |
| `internal/job/store_test.go` | Unit tests for MemStore |
| `internal/api/middleware_test.go` | Unit tests for ValidateUpload |
| `internal/api/handler_test.go` | Unit tests for handlers |
| `internal/worker/pool_test.go` | Unit tests for worker pool |

---

## Task 1: Project Scaffold

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `.env.example`

- [ ] **Step 1: Initialise the Go module**

```bash
cd /home/bhaskar/Pictures/bhaskar/projects/go-media-converter
go mod init go-media-converter
```

Expected output: creates `go.mod` with `module go-media-converter` and current Go version.

- [ ] **Step 2: Create directory structure**

```bash
mkdir -p cmd/server \
         internal/api \
         internal/config \
         internal/converter \
         internal/job \
         internal/worker \
         output \
         uploads
```

- [ ] **Step 3: Create .gitignore**

```
output/
uploads/
docs/docs.go
docs/swagger.json
docs/swagger.yaml
*.mp4
*.ts
```

Save as `.gitignore` in project root.

- [ ] **Step 4: Create .env.example**

```
PORT=8080
WORKER_COUNT=2
MAX_UPLOAD_BYTES=524288000
OUTPUT_DIR=./output
UPLOAD_DIR=./uploads
```

Save as `.env.example` in project root.

- [ ] **Step 5: Commit**

```bash
git init
git add go.mod .gitignore .env.example
git commit -m "chore: project scaffold"
```

---

## Task 2: Job Types

**Files:**
- Create: `internal/job/types.go`

- [ ] **Step 1: Write types.go**

```go
package job

import "time"

type Status string

const (
	StatusPending    Status = "pending"
	StatusProcessing Status = "processing"
	StatusDone       Status = "done"
	StatusFailed     Status = "failed"
)

type Job struct {
	ID        string    `json:"job_id"`
	Status    Status    `json:"status"`
	Error     string    `json:"error,omitempty"`
	HLSURL    string    `json:"hls_url,omitempty"`
	InputPath string    `json:"-"`
	OutputDir string    `json:"-"`
	CreatedAt time.Time `json:"-"`
}
```

Save as `internal/job/types.go`.

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/job/...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/job/types.go
git commit -m "feat: job types"
```

---

## Task 3: Job Store

**Files:**
- Create: `internal/job/store.go`
- Create: `internal/job/store_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package job_test

import (
	"errors"
	"testing"

	"go-media-converter/internal/job"
)

func TestMemStore_CreateAndGet(t *testing.T) {
	s := job.NewMemStore()
	j := job.Job{ID: "abc", Status: job.StatusPending}
	if err := s.Create(j); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "abc" {
		t.Errorf("got ID %q, want %q", got.ID, "abc")
	}
}

func TestMemStore_GetNotFound(t *testing.T) {
	s := job.NewMemStore()
	_, err := s.Get("missing")
	if !errors.Is(err, job.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestMemStore_Update(t *testing.T) {
	s := job.NewMemStore()
	j := job.Job{ID: "abc", Status: job.StatusPending}
	_ = s.Create(j)
	j.Status = job.StatusDone
	if err := s.Update(j); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get("abc")
	if got.Status != job.StatusDone {
		t.Errorf("got status %q, want %q", got.Status, job.StatusDone)
	}
}

func TestMemStore_UpdateNotFound(t *testing.T) {
	s := job.NewMemStore()
	err := s.Update(job.Job{ID: "missing"})
	if !errors.Is(err, job.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}
```

Save as `internal/job/store_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/job/... -v
```

Expected: FAIL — `job.NewMemStore`, `job.ErrNotFound` undefined.

- [ ] **Step 3: Write store.go**

```go
package job

import (
	"errors"
	"sync"
)

var ErrNotFound = errors.New("job not found")

type Store interface {
	Create(j Job) error
	Get(id string) (Job, error)
	Update(j Job) error
}

type MemStore struct {
	mu   sync.RWMutex
	jobs map[string]Job
}

func NewMemStore() *MemStore {
	return &MemStore{jobs: make(map[string]Job)}
}

func (s *MemStore) Create(j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[j.ID] = j
	return nil
}

func (s *MemStore) Get(id string) (Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return j, nil
}

func (s *MemStore) Update(j Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.jobs[j.ID]; !ok {
		return ErrNotFound
	}
	s.jobs[j.ID] = j
	return nil
}
```

Save as `internal/job/store.go`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/job/... -v
```

Expected: 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/job/store.go internal/job/store_test.go
git commit -m "feat: in-memory job store"
```

---

## Task 4: Config

**Files:**
- Create: `internal/config/config.go`

- [ ] **Step 1: Write config.go**

```go
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port           string
	WorkerCount    int
	MaxUploadBytes int64
	OutputDir      string
	UploadDir      string
}

func Load() Config {
	return Config{
		Port:           envOr("PORT", "8080"),
		WorkerCount:    envInt("WORKER_COUNT", 2),
		MaxUploadBytes: envInt64("MAX_UPLOAD_BYTES", 524288000),
		OutputDir:      envOr("OUTPUT_DIR", "./output"),
		UploadDir:      envOr("UPLOAD_DIR", "./uploads"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInt64(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
```

Save as `internal/config/config.go`.

- [ ] **Step 2: Verify it compiles**

```bash
go build ./internal/config/...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/config/config.go
git commit -m "feat: config from env vars"
```

---

## Task 5: FFmpeg Converter

**Files:**
- Create: `internal/converter/ffmpeg.go`

- [ ] **Step 1: Install uuid dependency first**

```bash
go get github.com/google/uuid
```

- [ ] **Step 2: Write ffmpeg.go**

```go
package converter

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Converter abstracts media conversion so the worker pool can use a mock in tests.
type Converter interface {
	Convert(inputPath, outputDir string) error
}

type FFmpeg struct{}

func New() *FFmpeg { return &FFmpeg{} }

func (f *FFmpeg) Convert(inputPath, outputDir string) error {
	segDir := filepath.Join(outputDir, "segments")
	if err := os.MkdirAll(segDir, 0755); err != nil {
		return fmt.Errorf("create segments dir: %w", err)
	}

	args := []string{
		"-y", "-i", inputPath,
		"-filter_complex", "[0:v]split=3[v1][v2][v3]",
		// 360p
		"-map", "[v1]", "-map", "0:a",
		"-vf", "scale=w=640:h=360",
		"-c:v", "libx264", "-c:a", "aac", "-b:v", "800k",
		"-hls_time", "6", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(segDir, "360p_%03d.ts"),
		filepath.Join(outputDir, "360p.m3u8"),
		// 720p
		"-map", "[v2]", "-map", "0:a",
		"-vf", "scale=w=1280:h=720",
		"-c:v", "libx264", "-c:a", "aac", "-b:v", "2800k",
		"-hls_time", "6", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(segDir, "720p_%03d.ts"),
		filepath.Join(outputDir, "720p.m3u8"),
		// 1080p
		"-map", "[v3]", "-map", "0:a",
		"-vf", "scale=w=1920:h=1080",
		"-c:v", "libx264", "-c:a", "aac", "-b:v", "5000k",
		"-hls_time", "6", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(segDir, "1080p_%03d.ts"),
		filepath.Join(outputDir, "1080p.m3u8"),
	}

	var stderr bytes.Buffer
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w\nstderr: %s", err, stderr.String())
	}

	return writeMaster(outputDir)
}

func writeMaster(outputDir string) error {
	const content = "#EXTM3U\n" +
		"#EXT-X-VERSION:3\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360\n" +
		"360p.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=2800000,RESOLUTION=1280x720\n" +
		"720p.m3u8\n" +
		"#EXT-X-STREAM-INF:BANDWIDTH=5000000,RESOLUTION=1920x1080\n" +
		"1080p.m3u8\n"
	return os.WriteFile(filepath.Join(outputDir, "master.m3u8"), []byte(content), 0644)
}
```

Save as `internal/converter/ffmpeg.go`.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/converter/...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/converter/ffmpeg.go go.mod go.sum
git commit -m "feat: ffmpeg converter with Converter interface"
```

---

## Task 6: Worker Pool

**Files:**
- Create: `internal/worker/pool.go`
- Create: `internal/worker/pool_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package worker_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go-media-converter/internal/job"
	"go-media-converter/internal/worker"
)

type mockConverter struct {
	err error
}

func (m *mockConverter) Convert(inputPath, outputDir string) error {
	if m.err != nil {
		return m.err
	}
	// write minimal output so the pool can set hls_url
	_ = os.MkdirAll(outputDir, 0755)
	_ = os.WriteFile(filepath.Join(outputDir, "master.m3u8"), []byte("#EXTM3U"), 0644)
	return nil
}

func waitForStatus(t *testing.T, store job.Store, id string, want job.Status) job.Job {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := store.Get(id)
		if j.Status == want {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for job %q to reach status %q", id, want)
	return job.Job{}
}

func TestPool_JobSucceeds(t *testing.T) {
	store := job.NewMemStore()
	pool := worker.New(store, &mockConverter{}, 1, 10)
	pool.Start()

	inputFile := filepath.Join(t.TempDir(), "input.mp4")
	_ = os.WriteFile(inputFile, []byte("fake"), 0644)

	j := job.Job{
		ID:        "job-ok",
		Status:    job.StatusPending,
		InputPath: inputFile,
		OutputDir: t.TempDir(),
	}
	_ = store.Create(j)
	pool.Enqueue("job-ok")

	got := waitForStatus(t, store, "job-ok", job.StatusDone)
	if got.HLSURL == "" {
		t.Error("expected non-empty HLSURL on done job")
	}
}

func TestPool_JobFails(t *testing.T) {
	store := job.NewMemStore()
	pool := worker.New(store, &mockConverter{err: errors.New("ffmpeg died")}, 1, 10)
	pool.Start()

	j := job.Job{
		ID:        "job-fail",
		Status:    job.StatusPending,
		InputPath: filepath.Join(t.TempDir(), "input.mp4"),
		OutputDir: t.TempDir(),
	}
	_ = store.Create(j)
	pool.Enqueue("job-fail")

	got := waitForStatus(t, store, "job-fail", job.StatusFailed)
	if got.Error == "" {
		t.Error("expected non-empty Error on failed job")
	}
}

func TestPool_PanicRecovered(t *testing.T) {
	store := job.NewMemStore()
	// converter panics
	panicker := &panicConverter{}
	pool := worker.New(store, panicker, 1, 10)
	pool.Start()

	j := job.Job{
		ID:        "job-panic",
		Status:    job.StatusPending,
		InputPath: filepath.Join(t.TempDir(), "input.mp4"),
		OutputDir: t.TempDir(),
	}
	_ = store.Create(j)
	pool.Enqueue("job-panic")

	got := waitForStatus(t, store, "job-panic", job.StatusFailed)
	if got.Error == "" {
		t.Error("expected non-empty Error after panic recovery")
	}
}

type panicConverter struct{}

func (p *panicConverter) Convert(_, _ string) error {
	panic("simulated panic")
}
```

Save as `internal/worker/pool_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/worker/... -v
```

Expected: FAIL — `worker.New`, `pool.Enqueue` undefined.

- [ ] **Step 3: Write pool.go**

```go
package worker

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"time"

	"go-media-converter/internal/converter"
	"go-media-converter/internal/job"
)

type Pool struct {
	store   job.Store
	conv    converter.Converter
	queue   chan string
	workers int
}

func New(store job.Store, conv converter.Converter, workers, queueSize int) *Pool {
	return &Pool{
		store:   store,
		conv:    conv,
		queue:   make(chan string, queueSize),
		workers: workers,
	}
}

func (p *Pool) Start() {
	for i := 0; i < p.workers; i++ {
		go p.run()
	}
}

func (p *Pool) Enqueue(jobID string) {
	p.queue <- jobID
}

func (p *Pool) run() {
	for jobID := range p.queue {
		p.process(jobID)
	}
}

func (p *Pool) process(jobID string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("worker panic", "job_id", jobID, "panic", r, "stack", string(debug.Stack()))
			p.markFailed(jobID, fmt.Sprintf("internal panic: %v", r))
		}
	}()

	j, err := p.store.Get(jobID)
	if err != nil {
		slog.Error("worker: job not found", "job_id", jobID)
		return
	}

	j.Status = job.StatusProcessing
	_ = p.store.Update(j)
	slog.Info("job started", "job_id", jobID)

	start := time.Now()
	convErr := p.conv.Convert(j.InputPath, j.OutputDir)
	_ = os.Remove(j.InputPath)

	if convErr != nil {
		slog.Error("job failed", "job_id", jobID, "error", convErr)
		_ = os.RemoveAll(j.OutputDir)
		p.markFailed(jobID, convErr.Error())
		return
	}

	j.Status = job.StatusDone
	j.HLSURL = "/hls/" + jobID + "/master.m3u8"
	_ = p.store.Update(j)
	slog.Info("job done", "job_id", jobID, "duration_ms", time.Since(start).Milliseconds())
}

func (p *Pool) markFailed(jobID, errMsg string) {
	j, err := p.store.Get(jobID)
	if err != nil {
		return
	}
	j.Status = job.StatusFailed
	j.Error = errMsg
	_ = p.store.Update(j)
}
```

Save as `internal/worker/pool.go`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/worker/... -v
```

Expected: 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/worker/pool.go internal/worker/pool_test.go
git commit -m "feat: worker pool with panic recovery"
```

---

## Task 7: Upload Middleware

**Files:**
- Create: `internal/api/middleware.go`
- Create: `internal/api/middleware_test.go`

- [ ] **Step 1: Install Gin**

```bash
go get github.com/gin-gonic/gin
```

- [ ] **Step 2: Write the failing tests**

```go
package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go-media-converter/internal/api"
)

func init() { gin.SetMode(gin.TestMode) }

func TestValidateUpload_TooLarge(t *testing.T) {
	r := gin.New()
	r.POST("/upload", api.ValidateUpload(10), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req.ContentLength = 11
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestValidateUpload_WithinLimit(t *testing.T) {
	r := gin.New()
	r.POST("/upload", api.ValidateUpload(100), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req.ContentLength = 50
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want %d", w.Code, http.StatusOK)
	}
}

func TestValidateUpload_UnknownSize(t *testing.T) {
	r := gin.New()
	r.POST("/upload", api.ValidateUpload(100), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req.ContentLength = -1 // unknown
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want %d", w.Code, http.StatusOK)
	}
}
```

Save as `internal/api/middleware_test.go`.

- [ ] **Step 3: Run tests to verify they fail**

```bash
go test ./internal/api/... -v
```

Expected: FAIL — `api.ValidateUpload` undefined.

- [ ] **Step 4: Write middleware.go**

```go
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func ValidateUpload(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error": "file too large",
				"code":  "FILE_TOO_LARGE",
			})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
```

Save as `internal/api/middleware.go`.

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/api/... -v
```

Expected: 3 tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/middleware.go internal/api/middleware_test.go go.mod go.sum
git commit -m "feat: upload size validation middleware"
```

---

## Task 8: HTTP Handlers

**Files:**
- Create: `internal/api/handler.go`
- Create: `internal/api/handler_test.go`

- [ ] **Step 1: Write the failing tests**

```go
package api_test

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"go-media-converter/internal/api"
	"go-media-converter/internal/job"
)

type mockEnqueuer struct{ ids []string }

func (m *mockEnqueuer) Enqueue(id string) { m.ids = append(m.ids, id) }

// minimalMP4 returns bytes that http.DetectContentType identifies as video/mp4.
func minimalMP4() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x18, // box size = 24
		0x66, 0x74, 0x79, 0x70, // "ftyp"
		0x69, 0x73, 0x6F, 0x6D, // major brand "isom"
		0x00, 0x00, 0x02, 0x00, // minor version
		0x69, 0x73, 0x6F, 0x6D, // compatible brand
		0x69, 0x73, 0x6F, 0x32, // compatible brand
	}
}

func TestHealth(t *testing.T) {
	store := job.NewMemStore()
	h := api.NewHandler(store, &mockEnqueuer{}, t.TempDir(), t.TempDir())
	r := gin.New()
	r.GET("/health", h.Health)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestGetJob_NotFound(t *testing.T) {
	store := job.NewMemStore()
	h := api.NewHandler(store, &mockEnqueuer{}, t.TempDir(), t.TempDir())
	r := gin.New()
	r.GET("/api/v1/jobs/:id", h.GetJob)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/missing", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

func TestGetJob_Found(t *testing.T) {
	store := job.NewMemStore()
	_ = store.Create(job.Job{ID: "abc", Status: job.StatusDone, HLSURL: "/hls/abc/master.m3u8"})
	h := api.NewHandler(store, &mockEnqueuer{}, t.TempDir(), t.TempDir())
	r := gin.New()
	r.GET("/api/v1/jobs/:id", h.GetJob)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
	var resp job.Job
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.ID != "abc" {
		t.Errorf("got ID %q, want %q", resp.ID, "abc")
	}
	if resp.HLSURL != "/hls/abc/master.m3u8" {
		t.Errorf("got hls_url %q, want /hls/abc/master.m3u8", resp.HLSURL)
	}
}

func TestUpload_MissingFile(t *testing.T) {
	store := job.NewMemStore()
	h := api.NewHandler(store, &mockEnqueuer{}, t.TempDir(), t.TempDir())
	r := gin.New()
	r.POST("/api/v1/upload", h.Upload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestUpload_InvalidMIME(t *testing.T) {
	store := job.NewMemStore()
	h := api.NewHandler(store, &mockEnqueuer{}, t.TempDir(), t.TempDir())
	r := gin.New()
	r.POST("/api/v1/upload", h.Upload)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.txt")
	_, _ = fw.Write([]byte("this is plain text, not a video"))
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", w.Code)
	}
}

func TestUpload_ValidMP4(t *testing.T) {
	store := job.NewMemStore()
	enq := &mockEnqueuer{}
	h := api.NewHandler(store, enq, t.TempDir(), t.TempDir())
	r := gin.New()
	r.POST("/api/v1/upload", h.Upload)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "test.mp4")
	_, _ = fw.Write(minimalMP4())
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("got %d, want 202", w.Code)
	}
	var resp map[string]string
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["job_id"] == "" {
		t.Error("expected non-empty job_id in response")
	}
	if len(enq.ids) != 1 {
		t.Errorf("expected 1 enqueued job, got %d", len(enq.ids))
	}
}
```

Save as `internal/api/handler_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/api/... -v
```

Expected: FAIL — `api.NewHandler`, `h.Upload`, etc. undefined.

- [ ] **Step 3: Write handler.go**

```go
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
// @Success      202   {object}  map[string]string  "job_id"
// @Failure      400   {object}  map[string]string  "MISSING_FILE or INVALID_MIME"
// @Failure      413   {object}  map[string]string  "FILE_TOO_LARGE"
// @Failure      500   {object}  map[string]string  "INTERNAL_ERROR"
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
// @Description  Returns the current status of a conversion job. When status is "done", hls_url is populated.
// @Tags         jobs
// @Produce      json
// @Param        id   path      string  true  "Job ID"
// @Success      200  {object}  job.Job
// @Failure      404  {object}  map[string]string  "JOB_NOT_FOUND"
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
// @Produce      application/vnd.apple.mpegurl
// @Param        job_id    path  string  true  "Job ID"
// @Param        filepath  path  string  true  "File path (e.g. master.m3u8 or segments/360p_000.ts)"
// @Success      200
// @Failure      404  {object}  map[string]string
// @Failure      409  {object}  map[string]string  "JOB_NOT_READY"
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
```

Save as `internal/api/handler.go`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/api/... -v
```

Expected: all 7 tests PASS (3 middleware + 5 handler — middleware_test.go reuses the same package).

Note: `TestUpload_ValidMP4` writes a real file to `t.TempDir()` so it needs disk access — this is expected.

- [ ] **Step 5: Commit**

```bash
git add internal/api/handler.go internal/api/handler_test.go
git commit -m "feat: http handlers with MIME validation"
```

---

## Task 9: Routes + Swagger Setup

**Files:**
- Create: `internal/api/routes.go`

- [ ] **Step 1: Install swaggo dependencies**

```bash
go get github.com/swaggo/gin-swagger
go get github.com/swaggo/files
go install github.com/swaggo/swag/cmd/swag@latest
```

Verify swag is available:
```bash
swag --version
```

Expected: prints swag version (e.g., `swag version v1.x.x`).

- [ ] **Step 2: Write routes.go**

```go
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
```

Save as `internal/api/routes.go`.

- [ ] **Step 3: Verify it compiles**

```bash
go build ./internal/api/...
```

Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add internal/api/routes.go go.mod go.sum
git commit -m "feat: route registration with swagger ui"
```

---

## Task 10: Main Entry Point + Swagger Annotations

**Files:**
- Create: `cmd/server/main.go`

- [ ] **Step 1: Write main.go**

```go
// Package main is the entry point for the Go Media Converter server.
//
// @title           Go Media Converter API
// @version         1.0
// @description     Converts uploaded MP4 files to adaptive bitrate HLS streams (360p/720p/1080p). Upload a file, receive a job_id, poll for status, then play the HLS stream.
//
// @contact.name    API Support
//
// @host            localhost:8080
// @BasePath        /
//
// @tag.name        jobs
// @tag.description Upload and track media conversion jobs
//
// @tag.name        hls
// @tag.description Serve HLS stream files
//
// @tag.name        system
// @tag.description Health and status endpoints
package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"

	"go-media-converter/internal/api"
	"go-media-converter/internal/config"
	"go-media-converter/internal/converter"
	"go-media-converter/internal/job"
	"go-media-converter/internal/worker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg := config.Load()

	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		slog.Error("cannot create output dir", "error", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(cfg.UploadDir, 0755); err != nil {
		slog.Error("cannot create upload dir", "error", err)
		os.Exit(1)
	}

	store := job.NewMemStore()
	conv := converter.New()
	pool := worker.New(store, conv, cfg.WorkerCount, 50)
	pool.Start()

	h := api.NewHandler(store, pool, cfg.OutputDir, cfg.UploadDir)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(gin.Logger())

	api.RegisterRoutes(r, h, cfg.MaxUploadBytes)

	slog.Info("server starting", "port", cfg.Port, "workers", cfg.WorkerCount)
	if err := r.Run(fmt.Sprintf(":%s", cfg.Port)); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}
```

Save as `cmd/server/main.go`.

- [ ] **Step 2: Verify it compiles (without docs yet — import will be added after swag init)**

```bash
go build ./cmd/server/...
```

Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add cmd/server/main.go
git commit -m "feat: server entry point"
```

---

## Task 11: Generate Swagger Docs

**Files:**
- Create: `docs/docs.go` (generated)
- Create: `docs/swagger.json` (generated)
- Create: `docs/swagger.yaml` (generated)
- Modify: `cmd/server/main.go` — add docs import
- Modify: `internal/api/routes.go` — already references swaggerFiles/ginSwagger

- [ ] **Step 1: Run swag init**

```bash
swag init -g cmd/server/main.go -o docs
```

Expected: creates `docs/docs.go`, `docs/swagger.json`, `docs/swagger.yaml`.

- [ ] **Step 2: Add docs import to main.go**

Add this blank import below the existing imports in `cmd/server/main.go`:

```go
import (
	"fmt"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"

	_ "go-media-converter/docs" // registers swagger spec

	"go-media-converter/internal/api"
	"go-media-converter/internal/config"
	"go-media-converter/internal/converter"
	"go-media-converter/internal/job"
	"go-media-converter/internal/worker"
)
```

- [ ] **Step 3: Build the full binary**

```bash
go build -o bin/server ./cmd/server/...
```

Expected: `bin/server` created, no errors.

- [ ] **Step 4: Start the server and verify Swagger UI**

```bash
./bin/server &
sleep 1
curl -s http://localhost:8080/health
```

Expected: `{"status":"ok"}`

Open `http://localhost:8080/swagger/index.html` in a browser — should show the Swagger UI with all three tag groups (jobs, hls, system).

Kill the background server:
```bash
kill %1
```

- [ ] **Step 5: Commit**

```bash
git add docs/ cmd/server/main.go go.mod go.sum bin/
git commit -m "feat: swagger docs generated"
```

Note: add `bin/` to `.gitignore` if you don't want to commit the binary:
```bash
echo "bin/" >> .gitignore
git add .gitignore
git commit -m "chore: ignore bin directory"
```

---

## Task 12: Run Full Test Suite

- [ ] **Step 1: Run all tests**

```bash
go test ./... -v
```

Expected: all tests in `internal/job`, `internal/api`, `internal/worker` pass. No failures.

- [ ] **Step 2: Verify swag is re-runnable after any handler annotation change**

```bash
swag init -g cmd/server/main.go -o docs && go build ./cmd/server/...
```

Expected: exits 0 cleanly.

- [ ] **Step 3: Final commit**

```bash
git add -A
git status
```

Verify no unexpected files are staged (no `.mp4`, no `output/`, no `uploads/`). Then:

```bash
git commit -m "chore: final cleanup and test verification"
```

---

## Quick-Start Reference

```bash
# Install deps
go mod tidy

# Run server (requires ffmpeg on PATH)
go run ./cmd/server

# Upload an MP4
curl -X POST http://localhost:8080/api/v1/upload \
  -F "file=@/path/to/video.mp4"
# → {"job_id":"<uuid>"}

# Poll status
curl http://localhost:8080/api/v1/jobs/<uuid>
# → {"job_id":"...","status":"done","hls_url":"/hls/<uuid>/master.m3u8"}

# Play HLS stream
# Point any HLS player at: http://localhost:8080/hls/<uuid>/master.m3u8

# Swagger UI
open http://localhost:8080/swagger/index.html
```
