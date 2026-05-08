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
