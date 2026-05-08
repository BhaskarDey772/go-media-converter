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
		"-filter_complex", "[0:v]split=3[v1][v2][v3];[v1]scale=w=640:h=360[s1];[v2]scale=w=1280:h=720[s2];[v3]scale=w=1920:h=1080[s3]",
		// 360p
		"-map", "[s1]", "-map", "0:a",
		"-c:v", "libx264", "-c:a", "aac", "-b:v", "800k",
		"-hls_time", "6", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(segDir, "360p_%03d.ts"),
		filepath.Join(outputDir, "360p.m3u8"),
		// 720p
		"-map", "[s2]", "-map", "0:a",
		"-c:v", "libx264", "-c:a", "aac", "-b:v", "2800k",
		"-hls_time", "6", "-hls_playlist_type", "vod",
		"-hls_segment_filename", filepath.Join(segDir, "720p_%03d.ts"),
		filepath.Join(outputDir, "720p.m3u8"),
		// 1080p
		"-map", "[s3]", "-map", "0:a",
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
