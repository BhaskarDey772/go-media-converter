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
	"github.com/joho/godotenv"

	_ "go-media-converter/docs" // registers swagger spec

	"go-media-converter/internal/api"
	"go-media-converter/internal/config"
	"go-media-converter/internal/converter"
	"go-media-converter/internal/job"
	"go-media-converter/internal/worker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	// Load .env if present; ignore error so production (no .env file) works fine
	_ = godotenv.Load()

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
