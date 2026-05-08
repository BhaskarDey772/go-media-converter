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

	defer os.Remove(j.InputPath)

	start := time.Now()
	convErr := p.conv.Convert(j.InputPath, j.OutputDir)

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
