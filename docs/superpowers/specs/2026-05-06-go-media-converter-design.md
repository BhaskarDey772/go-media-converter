# Go Media Converter — Design Spec

**Date:** 2026-05-06  
**Status:** Approved

---

## Overview

A Go HTTP server that accepts MP4 uploads, converts them to adaptive bitrate HLS (3 renditions), and serves the resulting stream files. No database or cloud storage in this phase — all state is in-memory and all files are on local disk.

---

## Architecture

Single Go binary: HTTP server + in-process goroutine worker pool. FFmpeg is invoked via `os/exec`. Job state is held in an in-memory store behind an interface so the DB swap-in is clean later.

### Project Structure

```
go-media-converter/
├── cmd/server/main.go          # entry point, wires everything
├── internal/
│   ├── api/
│   │   ├── handler.go          # HTTP handlers
│   │   ├── middleware.go       # file size + MIME validation
│   │   └── routes.go           # route registration
│   ├── converter/
│   │   └── ffmpeg.go           # ffmpeg exec wrapper, HLS logic
│   ├── job/
│   │   ├── store.go            # in-memory job store (interface + impl)
│   │   └── types.go            # Job struct, status enum
│   └── worker/
│       └── pool.go             # goroutine worker pool
├── docs/
│   └── swagger.yaml            # OpenAPI 3.0 spec
├── output/                     # created at runtime, gitignored
├── uploads/                    # created at runtime, gitignored
├── go.mod
└── go.sum
```

### Dependencies

| Package | Purpose |
|---|---|
| `github.com/gin-gonic/gin` | HTTP router |
| `github.com/swaggo/gin-swagger` | Swagger UI middleware |
| `github.com/swaggo/swag` | Swagger annotation codegen |
| `github.com/google/uuid` | Job IDs |
| FFmpeg (host/container) | Media conversion via `os/exec` |
| `log/slog` (stdlib) | Structured JSON logging |

---

## API Endpoints

```
POST   /api/v1/upload           Upload MP4, returns job_id
GET    /api/v1/jobs/:id         Poll job status + HLS URL when done
GET    /hls/:job_id/master.m3u8 Serve HLS master playlist
GET    /hls/:job_id/:file       Serve HLS segments and rendition playlists
GET    /swagger/*any            Swagger UI
GET    /health                  Health check
```

### POST /api/v1/upload

- **Content-Type:** `multipart/form-data`
- **Field:** `file` (MP4 video)
- **Validation:** MIME must be `video/mp4` (checked from first 512 bytes), size ≤ 500MB
- **Response 202:**
```json
{
  "job_id": "550e8400-e29b-41d4-a716-446655440000"
}
```

### GET /api/v1/jobs/:id

**Response:**
```json
{
  "job_id": "uuid",
  "status": "pending | processing | done | failed",
  "error": "set only on failed",
  "hls_url": "/hls/<job_id>/master.m3u8"
}
```
`hls_url` is only present when `status` is `done`.

### Error shape (all endpoints)

```json
{
  "error": "human readable message",
  "code": "ERROR_CODE"
}
```

---

## HLS Output

### Disk Layout

```
output/<job_id>/
├── master.m3u8
├── 360p.m3u8
├── 720p.m3u8
├── 1080p.m3u8
└── segments/
    ├── 360p_000.ts  360p_001.ts  ...
    ├── 720p_000.ts  720p_001.ts  ...
    └── 1080p_000.ts 1080p_001.ts ...
```

### Rendition Profiles

| Name | Resolution | Video Bitrate |
|---|---|---|
| 360p | 640×360 | 800k |
| 720p | 1280×720 | 2800k |
| 1080p | 1920×1080 | 5000k |

Audio: AAC for all renditions. Segment duration: 6 seconds. Playlist type: VOD.

### FFmpeg Command

One invocation, three outputs via `-filter_complex split`:

```bash
ffmpeg -i input.mp4 \
  -filter_complex "[0:v]split=3[v1][v2][v3]" \
  -map "[v1]" -map 0:a -vf scale=w=640:h=360  -c:v h264 -c:a aac -b:v 800k  \
    -hls_time 6 -hls_playlist_type vod \
    -hls_segment_filename output/<id>/segments/360p_%03d.ts  output/<id>/360p.m3u8 \
  -map "[v2]" -map 0:a -vf scale=w=1280:h=720  -c:v h264 -c:a aac -b:v 2800k \
    -hls_time 6 -hls_playlist_type vod \
    -hls_segment_filename output/<id>/segments/720p_%03d.ts  output/<id>/720p.m3u8 \
  -map "[v3]" -map 0:a -vf scale=w=1920:h=1080 -c:v h264 -c:a aac -b:v 5000k \
    -hls_time 6 -hls_playlist_type vod \
    -hls_segment_filename output/<id>/segments/1080p_%03d.ts output/<id>/1080p.m3u8
```

After FFmpeg exits (exit code 0), Go writes `master.m3u8` referencing the three rendition playlists.

---

## Worker Pool

- Goroutine pool, size controlled by `WORKER_COUNT` env var (default: 2)
- Buffered job ID channel as queue (buffer: 50)
- Each worker: fetch job from store → run FFmpeg → update status to `done` or `failed`
- Panics in workers are recovered; job is marked `failed`
- Uploaded MP4 deleted from `uploads/` after conversion finishes (success or failure)
- On failure: output directory is cleaned up; FFmpeg stderr stored in job `error` field

---

## Configuration

| Env Var | Default | Description |
|---|---|---|
| `PORT` | `8080` | HTTP listen port |
| `WORKER_COUNT` | `2` | Parallel FFmpeg workers |
| `MAX_UPLOAD_BYTES` | `524288000` | Upload size limit (500MB) |
| `OUTPUT_DIR` | `./output` | Base dir for HLS output |
| `UPLOAD_DIR` | `./uploads` | Temp dir for incoming MP4s |

---

## Observability

Structured JSON logs via `log/slog` (stdlib). Log events:

- Server start (port, worker count)
- Job enqueued (job_id)
- Job started (job_id)
- Job done (job_id, duration)
- Job failed (job_id, error)
- File served (job_id, file)

---

## Out of Scope (this phase)

- Database persistence (job state lost on restart)
- S3 or any cloud storage
- Authentication beyond MIME/size validation
- Job cancellation
- Cleanup of old output directories
