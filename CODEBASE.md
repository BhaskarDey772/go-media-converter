# Go Media Converter — Codebase Guide

> Written for a Node.js developer learning Go. Every concept is explained by comparing it to something you already know.

---

## Table of Contents

1. [How Go is different from Node.js](#1-how-go-is-different-from-nodejs)
2. [Project structure](#2-project-structure)
3. [Startup — `cmd/server/main.go`](#3-startup--cmdservermaingofile)
4. [Config — `internal/config/config.go`](#4-config--internalconfigconfiggo)
5. [Job types — `internal/job/types.go`](#5-job-types--internaljobtybesgo)
6. [Job store — `internal/job/store.go`](#6-job-store--internaljobstorgo)
7. [FFmpeg converter — `internal/converter/ffmpeg.go`](#7-ffmpeg-converter--internalconverterffmpeggo)
8. [Worker pool — `internal/worker/pool.go`](#8-worker-pool--internalworkerpoolgo)
9. [Middleware — `internal/api/middleware.go`](#9-middleware--internalapiMiddlewarego)
10. [HTTP handlers — `internal/api/handler.go`](#10-http-handlers--internalapihandlergo)
11. [Routes — `internal/api/routes.go`](#11-routes--internalapiroutesgo)
12. [Full request lifecycle](#12-full-request-lifecycle)
13. [API quick-reference](#13-api-quick-reference)

---

## 1. How Go is different from Node.js

| Concept | Node.js | Go |
|---|---|---|
| Running code | `node index.js` or `ts-node` | Compiled to a single binary (`go build`) |
| Package manager | `npm` / `yarn` | `go get` (dependencies in `go.mod`) |
| Async pattern | `async/await`, Promises | Goroutines (`go func()`) + channels |
| Types | Optional (TypeScript) | Always required, checked at compile time |
| `null` | `null`, `undefined` | Zero values (`""`, `0`, `nil`) |
| Error handling | `try/catch` or `.catch()` | Functions return `(value, error)` — you check the error manually |
| `require` / `import` | `const x = require('x')` | `import "x"` |
| Interfaces | TypeScript `interface` | Go `interface` — but **implicit** (no `implements` keyword) |
| Classes | `class Foo {}` | `type Foo struct {}` + methods |
| `this` | `this` inside class methods | The receiver variable (e.g. `h` in `func (h *Handler) Upload(...)`) |
| Structs | Plain objects `{}` | `type Foo struct { Field string }` |
| `defer` | No direct equivalent | Runs a function when the current function exits (like `finally`) |

---

## 2. Project structure

```
go-media-converter/
│
├── cmd/
│   └── server/
│       └── main.go          ← Entry point. Like index.js in Node.
│
├── internal/                ← Private packages. Cannot be imported by outside code.
│   ├── config/
│   │   └── config.go        ← Reads .env / environment variables
│   ├── job/
│   │   ├── types.go         ← Defines what a "Job" looks like (like a TypeScript type)
│   │   └── store.go         ← In-memory database for jobs (like a Map)
│   ├── converter/
│   │   └── ffmpeg.go        ← Runs FFmpeg to convert MP4 → HLS
│   ├── worker/
│   │   └── pool.go          ← Background workers (like Node.js worker threads)
│   └── api/
│       ├── middleware.go    ← Request validation (runs before the handler)
│       ├── handler.go       ← HTTP handlers (like Express route callbacks)
│       └── routes.go        ← Registers all routes (like app.use() in Express)
│
├── output/                  ← Generated HLS files live here
├── uploads/                 ← Temporary uploaded MP4 files
├── .env.example             ← Environment variable template
└── go.mod                   ← Like package.json
```

> **`internal/` in Go** means those packages are private to this module — other Go projects can't import them. It's Go's way of enforcing encapsulation.

---

## 3. Startup — `cmd/server/main.go`

**Node.js equivalent:** `index.js` or `server.js`

```go
func main() {
    // 1. Set up structured JSON logging (like winston/pino in Node)
    slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

    // 2. Load .env file (like require('dotenv').config())
    _ = godotenv.Load()

    // 3. Read config from environment variables
    cfg := config.Load()

    // 4. Create output and upload directories if they don't exist
    os.MkdirAll(cfg.OutputDir, 0755)
    os.MkdirAll(cfg.UploadDir, 0755)

    // 5. Create the in-memory job store (like a Map to track jobs)
    store := job.NewMemStore()

    // 6. Create the FFmpeg converter
    conv := converter.New()

    // 7. Create and start the worker pool (like a queue processor)
    pool := worker.New(store, conv, cfg.WorkerCount, 50)
    pool.Start()

    // 8. Create the HTTP handler (like a controller in Express)
    h := api.NewHandler(store, pool, cfg.OutputDir, cfg.UploadDir)

    // 9. Create the Gin router (like express())
    r := gin.New()
    r.Use(gin.Recovery()) // catches panics, like Express error middleware
    r.Use(gin.Logger())   // logs each request

    // 10. Register all routes
    api.RegisterRoutes(r, h, cfg.MaxUploadBytes)

    // 11. Start listening
    r.Run(":8080")
}
```

### Key differences from Node.js

- `_ = godotenv.Load()` — the `_` means "I'm deliberately ignoring the error". In Go you must acknowledge every return value. Here we ignore the error because if `.env` doesn't exist (production), that's fine.
- `0755` — Unix file permission (owner: read/write/execute, group: read/execute, others: read/execute). You'll see this when creating directories.
- `go func()` inside `pool.Start()` is how goroutines work — like spawning a new async worker, but extremely lightweight.

---

## 4. Config — `internal/config/config.go`

**Node.js equivalent:** `process.env.PORT || '8080'`

```go
type Config struct {
    Port           string  // PORT env var
    WorkerCount    int     // WORKER_COUNT env var
    MaxUploadBytes int64   // MAX_UPLOAD_BYTES env var
    OutputDir      string  // OUTPUT_DIR env var
    UploadDir      string  // UPLOAD_DIR env var
}

func Load() Config {
    return Config{
        Port:           envOr("PORT", "8080"),
        WorkerCount:    envInt("WORKER_COUNT", 2),
        MaxUploadBytes: envInt64("MAX_UPLOAD_BYTES", 524288000), // 500MB
        OutputDir:      envOr("OUTPUT_DIR", "./output"),
        UploadDir:      envOr("UPLOAD_DIR", "./uploads"),
    }
}
```

### Helper functions

```go
func envOr(key, def string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return def
}
```

This is exactly `process.env.KEY || default` in Node.js.

```go
func envInt(key string, def int) int {
    if v := os.Getenv(key); v != "" {
        if n, err := strconv.Atoi(v); err == nil {
            return n
        }
    }
    return def
}
```

`strconv.Atoi` converts a string to an integer — like `parseInt()` in JavaScript. In Go, it returns `(int, error)`, so we check `err == nil` before trusting the result.

---

## 5. Job types — `internal/job/types.go`

**Node.js equivalent:** A TypeScript type/interface

```go
// Status is just a string type with named constants.
// In TypeScript this would be: type Status = 'pending' | 'processing' | 'done' | 'failed'
type Status string

const (
    StatusPending    Status = "pending"
    StatusProcessing Status = "processing"
    StatusDone       Status = "done"
    StatusFailed     Status = "failed"
)

type Job struct {
    ID        string    `json:"job_id"`    // sent as "job_id" in JSON
    Status    Status    `json:"status"`
    Error     string    `json:"error,omitempty"`    // omitted from JSON if empty
    HLSURL    string    `json:"hls_url,omitempty"`  // omitted from JSON if empty
    InputPath string    `json:"-"`   // "-" means NEVER include in JSON
    OutputDir string    `json:"-"`
    CreatedAt time.Time `json:"-"`
}
```

### Struct tags explained

Those backtick strings after each field are **struct tags** — metadata about the field.

| Tag | Meaning |
|---|---|
| `` `json:"job_id"` `` | In JSON output, call this field `job_id` instead of `ID` |
| `` `json:"error,omitempty"` `` | Only include in JSON if the value is not empty |
| `` `json:"-"` `` | Never include this field in JSON output at all |

In Node.js/TypeScript, you'd use `toJSON()` or a serialisation library to control this. Go builds it directly into the type.

---

## 6. Job store — `internal/job/store.go`

**Node.js equivalent:** A `Map<string, Job>` wrapped in a class, but thread-safe.

### The interface

```go
// An interface in Go is like a TypeScript interface, but you never write "implements".
// Any type that has these 3 methods automatically satisfies this interface.
type Store interface {
    Create(j Job) error
    Get(id string) (Job, error)
    Update(j Job) error
}
```

> In TypeScript: `interface Store { create(j: Job): void; get(id: string): Job | null; update(j: Job): void; }`

### Why an interface?

Right now we use `MemStore` (a `map` in memory). Later when we add a database, we just write a new type that has these 3 methods and swap it in — without changing any other file.

### MemStore — the actual implementation

```go
type MemStore struct {
    mu   sync.RWMutex   // a lock to prevent race conditions
    jobs map[string]Job  // like: const jobs = new Map<string, Job>()
}

func NewMemStore() *MemStore {
    return &MemStore{jobs: make(map[string]Job)}
    // like: return { jobs: new Map() }
}
```

### Why do we need a lock (`sync.RWMutex`)?

In Node.js you have one thread, so a `Map` is safe. In Go, multiple goroutines (workers) run **truly in parallel** and can read/write the map at the same time. Without a lock, two goroutines writing simultaneously would corrupt the data (a "data race").

`RWMutex` has two modes:
- `Lock()` / `Unlock()` — exclusive write lock. Only one goroutine can write at a time.
- `RLock()` / `RUnlock()` — shared read lock. Multiple goroutines can read simultaneously.

```go
func (s *MemStore) Get(id string) (Job, error) {
    s.mu.RLock()          // allow multiple concurrent reads
    defer s.mu.RUnlock()  // always unlock when function returns
    j, ok := s.jobs[id]
    if !ok {
        return Job{}, ErrNotFound  // return empty Job + the error
    }
    return j, nil  // return the job + no error
}
```

### `defer` explained

`defer s.mu.RUnlock()` means: "run `RUnlock()` when this function exits, no matter how it exits (normal return, early return, panic)". It's like `finally` in JavaScript's `try/finally`.

### Error handling in Go

In Node you'd throw an error. In Go, errors are just return values:

```go
// Node.js:
function get(id) {
    if (!jobs.has(id)) throw new Error('not found')
    return jobs.get(id)
}

// Go:
func (s *MemStore) Get(id string) (Job, error) {
    j, ok := s.jobs[id]
    if !ok {
        return Job{}, ErrNotFound
    }
    return j, nil
}

// The caller always checks:
j, err := store.Get("some-id")
if err != nil {
    // handle the error
}
```

---

## 7. FFmpeg converter — `internal/converter/ffmpeg.go`

**Node.js equivalent:** `child_process.spawn('ffmpeg', [...])`

### The interface

```go
type Converter interface {
    Convert(inputPath, outputDir string) error
}
```

`FFmpeg` struct satisfies this interface. This means in tests, a fake "converter" can be used without needing a real video file.

### `Convert()` — step by step

```go
func (f *FFmpeg) Convert(inputPath, outputDir string) error {
    // 1. Create the segments subdirectory
    segDir := filepath.Join(outputDir, "segments")
    os.MkdirAll(segDir, 0755)

    // 2. Build the FFmpeg arguments as a slice of strings
    //    (like: ['ffmpeg', '-y', '-i', 'input.mp4', ...])
    args := []string{
        "-y",                          // overwrite output without asking
        "-i", inputPath,               // input file
        "-filter_complex", "[0:v]split=3[v1][v2][v3]",  // split video into 3 streams
        // ... 3 sets of output options for 360p, 720p, 1080p
    }

    // 3. Capture FFmpeg's stderr (error output) into a buffer
    var stderr bytes.Buffer
    cmd := exec.Command("ffmpeg", args...)
    cmd.Stderr = &stderr

    // 4. Run FFmpeg and wait for it to finish
    if err := cmd.Run(); err != nil {
        // FFmpeg failed — include its error output in our error message
        return fmt.Errorf("ffmpeg: %w\nstderr: %s", err, stderr.String())
    }

    // 5. Write the master playlist file
    return writeMaster(outputDir)
}
```

### FFmpeg filter explained

```
-filter_complex "[0:v]split=3[v1][v2][v3]"
```

This takes the single video stream (`[0:v]`) and splits it into 3 identical copies (`[v1]`, `[v2]`, `[v3]`). Each copy is then encoded at a different resolution. This is more efficient than running FFmpeg 3 times.

### `writeMaster()` — creates the HLS master playlist

```go
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

An HLS player (VLC, Safari, hls.js) reads `master.m3u8` and automatically picks the right quality based on the viewer's bandwidth.

---

## 8. Worker pool — `internal/worker/pool.go`

**Node.js equivalent:** Bull/BullMQ queue with workers, or `worker_threads`

### Why a worker pool?

FFmpeg conversion takes seconds or minutes. You can't block the HTTP request while waiting — the client would time out. So the upload handler returns immediately with a job ID, and the actual conversion happens in the background.

In Node.js you'd use Bull queue. In Go, we use goroutines and a channel.

### The Pool struct

```go
type Pool struct {
    store   job.Store           // to update job status
    conv    converter.Converter // to run FFmpeg
    queue   chan string          // a channel of job IDs (like a queue)
    workers int                 // number of parallel goroutines
}
```

### `chan string` — Go channels

A channel is a thread-safe queue. You put a value in with `<-`, and a goroutine on the other end receives it.

```go
// Send a job ID into the channel:
p.queue <- "some-uuid"

// Receive from the channel (blocks until something arrives):
jobID := <-p.queue
```

This is the core mechanism. The HTTP handler sends job IDs into the channel. The worker goroutines receive and process them.

### `Start()` — launches the goroutines

```go
func (p *Pool) Start() {
    for i := 0; i < p.workers; i++ {
        go p.run()  // "go" keyword starts a goroutine (extremely cheap thread)
    }
}
```

`go p.run()` is like `new Worker(...)` or `setImmediate(() => worker())` but much cheaper. Go can run millions of goroutines.

### `run()` — the goroutine's loop

```go
func (p *Pool) run() {
    for jobID := range p.queue {  // blocks waiting for the next job ID
        p.process(jobID)
    }
}
```

`range p.queue` keeps looping forever, blocking until a new job ID appears in the channel. This is like `queue.process(async (job) => { ... })` in Bull.

### `process()` — the core logic

```go
func (p *Pool) process(jobID string) {
    // 1. Panic recovery — if something crashes, catch it and mark the job failed
    defer func() {
        if r := recover(); r != nil {
            p.markFailed(jobID, fmt.Sprintf("internal panic: %v", r))
        }
    }()

    // 2. Load the job from the store
    j, err := p.store.Get(jobID)

    // 3. Mark as "processing"
    j.Status = job.StatusProcessing
    p.store.Update(j)

    // 4. Always delete the uploaded MP4 when done (defer = like finally)
    defer os.Remove(j.InputPath)

    // 5. Run FFmpeg
    convErr := p.conv.Convert(j.InputPath, j.OutputDir)

    // 6. Handle failure
    if convErr != nil {
        os.RemoveAll(j.OutputDir)  // clean up partial output
        p.markFailed(jobID, convErr.Error())
        return
    }

    // 7. Mark as done
    j.Status = job.StatusDone
    j.HLSURL = "/hls/" + jobID + "/master.m3u8"
    p.store.Update(j)
}
```

### `recover()` explained

In Node.js, an uncaught exception in a worker thread crashes it. In Go, a `panic` (uncaught error) kills the whole program. `recover()` inside a `defer` catches the panic and lets you handle it gracefully — here we just mark the job as failed so the server keeps running.

---

## 9. Middleware — `internal/api/middleware.go`

**Node.js equivalent:** Express middleware `(req, res, next) => {}`

```go
func ValidateUpload(maxBytes int64) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Check Content-Length header before reading the body at all
        if c.Request.ContentLength > maxBytes {
            c.AbortWithStatusJSON(413, gin.H{
                "error": "file too large",
                "code":  "FILE_TOO_LARGE",
            })
            return  // stop here, don't call next handler
        }
        // Wrap the body reader so it can't read more than maxBytes
        c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
        c.Next()  // like next() in Express — continue to the actual handler
    }
}
```

`ValidateUpload(maxBytes)` is a **function that returns a function** — a closure, exactly like middleware factories in Express:

```js
// Node.js equivalent:
function validateUpload(maxBytes) {
    return (req, res, next) => {
        if (req.headers['content-length'] > maxBytes) {
            return res.status(413).json({ error: 'file too large' })
        }
        next()
    }
}
```

---

## 10. HTTP handlers — `internal/api/handler.go`

**Node.js equivalent:** Express route handler functions / controller methods

### The `Enqueuer` interface

```go
type Enqueuer interface {
    Enqueue(jobID string)
}
```

The `Handler` struct talks to the worker pool through this interface. The pool has an `Enqueue` method so it satisfies this interface automatically. This is Go's "duck typing" — if it has the method, it qualifies. No `implements` keyword needed.

### The Handler struct

```go
type Handler struct {
    store     job.Store  // to read/write jobs
    pool      Enqueuer   // to submit new jobs for conversion
    outputDir string     // where HLS files are stored
    uploadDir string     // where uploaded MP4s are saved
}
```

Like a class in Node.js with injected dependencies (dependency injection).

### `Upload()` — POST /api/v1/upload

```go
func (h *Handler) Upload(c *gin.Context) {
    // 1. Get the uploaded file from the multipart form
    fh, err := c.FormFile("file")
    // fh is a *multipart.FileHeader — like req.file in multer

    // 2. Open the file to read its bytes
    f, err := fh.Open()
    defer f.Close()

    // 3. Read first 512 bytes to detect the real MIME type
    buf := make([]byte, 512)  // make([]byte, 512) = new Buffer(512) in Node
    n, _ := f.Read(buf)
    mime := http.DetectContentType(buf[:n])
    // buf[:n] = buf.slice(0, n) in JavaScript

    if mime != "video/mp4" {
        c.JSON(400, gin.H{"error": "file must be video/mp4", "code": "INVALID_MIME"})
        return
    }

    // 4. Generate a unique ID for this job
    jobID := uuid.New().String()  // like crypto.randomUUID() in Node

    // 5. Save the MP4 to disk
    inputPath := filepath.Join(h.uploadDir, jobID+".mp4")
    c.SaveUploadedFile(fh, inputPath)

    // 6. Create the job record
    j := job.Job{
        ID:        jobID,
        Status:    job.StatusPending,
        InputPath: inputPath,
        OutputDir: filepath.Join(h.outputDir, jobID),
        CreatedAt: time.Now(),
    }
    h.store.Create(j)

    // 7. Send the job to the worker pool
    h.pool.Enqueue(jobID)

    // 8. Return 202 Accepted (not 200 — work is not done yet)
    c.JSON(202, gin.H{"job_id": jobID})
}
```

### `GetJob()` — GET /api/v1/jobs/:id

```go
func (h *Handler) GetJob(c *gin.Context) {
    id := c.Param("id")  // like req.params.id in Express
    j, err := h.store.Get(id)
    if err != nil {
        c.JSON(404, gin.H{"error": "job not found", "code": "JOB_NOT_FOUND"})
        return
    }
    c.JSON(200, j)  // Go auto-serialises the struct to JSON
}
```

### `ServeHLS()` — GET /hls/:job_id/*filepath

```go
func (h *Handler) ServeHLS(c *gin.Context) {
    jobID := c.Param("job_id")
    filePath := c.Param("filepath")  // "*filepath" captures everything, e.g. "/master.m3u8"

    j, err := h.store.Get(jobID)
    if err != nil { /* 404 */ }

    if j.Status != job.StatusDone {
        c.JSON(409, gin.H{"error": "job not complete", "code": "JOB_NOT_READY"})
        return
    }

    fullPath := filepath.Join(h.outputDir, jobID, filePath)

    if filepath.Ext(filePath) == ".m3u8" {
        // HLS players need this specific Content-Type
        c.Header("Content-Type", "application/vnd.apple.mpegurl")
    }

    c.File(fullPath)  // streams the file from disk to the client
}
```

---

## 11. Routes — `internal/api/routes.go`

**Node.js equivalent:** Express router setup

```go
func RegisterRoutes(r *gin.Engine, h *Handler, maxUploadBytes int64) {
    r.GET("/health", h.Health)
    r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

    // Route group — like express.Router() with a prefix
    v1 := r.Group("/api/v1")
    {
        v1.POST("/upload", ValidateUpload(maxUploadBytes), h.Upload)
        //       ↑ path        ↑ middleware                 ↑ handler
        v1.GET("/jobs/:id", h.GetJob)
    }

    r.GET("/hls/:job_id/*filepath", h.ServeHLS)
    //                    ↑ wildcard — matches any path after job_id
}
```

**Node.js equivalent:**

```js
router.post('/api/v1/upload', validateUpload(maxBytes), handlers.upload)
router.get('/api/v1/jobs/:id', handlers.getJob)
router.get('/hls/:jobId/*', handlers.serveHLS)
```

---

## 12. Full request lifecycle

Here is everything that happens when a client uploads a video, step by step:

```
1. Client: POST /api/v1/upload  (multipart form, file field = video.mp4)
           │
2. Gin:    Passes through gin.Logger() and gin.Recovery() middleware
           │
3. Middleware: ValidateUpload(500MB)
           │   Checks Content-Length header
           │   If too large → 413 response, stops here
           │   Otherwise: wraps body with MaxBytesReader, calls Next()
           │
4. Handler: Upload()
           │   Opens the file, reads first 512 bytes
           │   http.DetectContentType() → "video/mp4"?
           │   No  → 400 INVALID_MIME, stops here
           │   Yes → continues
           │
           │   Generates UUID (e.g. "abc-123")
           │   Saves file to: uploads/abc-123.mp4
           │   Creates dir:   output/abc-123/
           │
           │   Creates Job{ id: "abc-123", status: "pending", ... }
           │   Saves job in MemStore
           │
           │   Sends "abc-123" into the worker channel
           │   Returns 202: { "job_id": "abc-123" }
           │
5. Client: Receives 202 immediately. Starts polling:
           GET /api/v1/jobs/abc-123
           → { "job_id": "abc-123", "status": "pending" }
           (keeps polling every second or so)
           │
6. Worker goroutine (running in parallel):
           │   Receives "abc-123" from the channel
           │   Updates status → "processing" in MemStore
           │   Calls FFmpeg:
           │       ffmpeg -i uploads/abc-123.mp4 \
           │           [split into 3 streams] \
           │           output/abc-123/360p.m3u8
           │           output/abc-123/720p.m3u8
           │           output/abc-123/1080p.m3u8
           │       writes output/abc-123/master.m3u8
           │   Deletes uploads/abc-123.mp4 (no longer needed)
           │   Updates status → "done", hls_url → "/hls/abc-123/master.m3u8"
           │
7. Client: Next poll returns:
           { "job_id": "abc-123", "status": "done", "hls_url": "/hls/abc-123/master.m3u8" }
           │
8. Client: HLS player opens:
           GET /hls/abc-123/master.m3u8
           → Player reads master.m3u8, picks best quality
           GET /hls/abc-123/720p.m3u8
           → Player reads segment list
           GET /hls/abc-123/segments/720p_000.ts
           GET /hls/abc-123/segments/720p_001.ts
           → Video plays
```

---

## 13. API quick-reference

| Method | Path | What it does |
|---|---|---|
| `POST` | `/api/v1/upload` | Upload MP4. Returns `{ job_id }`. |
| `GET` | `/api/v1/jobs/:id` | Poll job status. Returns job object. |
| `GET` | `/hls/:job_id/master.m3u8` | HLS master playlist (all qualities). |
| `GET` | `/hls/:job_id/360p.m3u8` | 360p rendition playlist. |
| `GET` | `/hls/:job_id/720p.m3u8` | 720p rendition playlist. |
| `GET` | `/hls/:job_id/1080p.m3u8` | 1080p rendition playlist. |
| `GET` | `/hls/:job_id/segments/*.ts` | Video segment files. |
| `GET` | `/health` | Health check. Returns `{ status: "ok" }`. |
| `GET` | `/swagger/index.html` | Interactive API docs. |

### Job status values

| Status | Meaning |
|---|---|
| `pending` | Job created, waiting for a worker to pick it up |
| `processing` | FFmpeg is running |
| `done` | Conversion complete, `hls_url` is set |
| `failed` | FFmpeg failed, `error` field explains why |

### Example curl commands

```bash
# Upload
curl -X POST http://localhost:8080/api/v1/upload \
  -F "file=@/path/to/video.mp4"

# Poll
curl http://localhost:8080/api/v1/jobs/<job_id>

# Play in VLC
vlc http://localhost:8080/hls/<job_id>/master.m3u8
```

### Environment variables (`.env`)

```
PORT=8080              # Server port
WORKER_COUNT=2         # Parallel FFmpeg conversions
MAX_UPLOAD_BYTES=524288000  # 500MB upload limit
OUTPUT_DIR=./output    # Where HLS files are stored
UPLOAD_DIR=./uploads   # Where uploaded MP4s are temporarily saved
```
