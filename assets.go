package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"bytes"
)

type FFProbeOutput struct {
	Streams []struct {
		Width      int    `json:"width"`
		Height     int    `json:"height"`
		AspectRatio string `json:"display_aspect_ratio"`
	} `json:"streams"`
}

func getVideoAspectRatio(filePath string) (string, error) {

	cmd := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", filePath)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return "", err
	}

	var ffprobeOutput FFProbeOutput
	err = json.Unmarshal(out.Bytes(), &ffprobeOutput)
	if err != nil {
		return "", err
	}

	for _, stream := range ffprobeOutput.Streams {
		if stream.Width != 0 && stream.Height != 0 {
			return stream.AspectRatio, nil
		}
	}

	return "", fmt.Errorf("no video stream found")	
}

func processVideoForFastStart(filePath string) (string, error) {
	outputPath := filePath + "_faststart.mp4"
	cmd := exec.Command("ffmpeg", "-i", filePath, "-c", "copy", "-movflags", "faststart", "-f", "mp4", outputPath)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return outputPath, nil

}

func (cfg apiConfig) ensureAssetsDir() error {
	if _, err := os.Stat(cfg.assetsRoot); os.IsNotExist(err) {
		return os.Mkdir(cfg.assetsRoot, 0755)
	}
	return nil
}

func getAssetPath(mediaType string) string {
	ext := mediaTypeToExt(mediaType)
	uniqueID := make([]byte, 32)
	rand.Read(uniqueID)
	videoID := base64.RawURLEncoding.EncodeToString(uniqueID)

	return fmt.Sprintf("%s%s", videoID, ext)
}

func (cfg apiConfig) getAssetDiskPath(assetPath string) string {
	return filepath.Join(cfg.assetsRoot, assetPath)
}

func (cfg apiConfig) getAssetURL(assetPath string) string {
	return fmt.Sprintf("http://localhost:%s/assets/%s", cfg.port, assetPath)
}

func mediaTypeToExt(mediaType string) string {
	parts := strings.Split(mediaType, "/")
	if len(parts) != 2 {
		return ".bin"
	}
	return "." + parts[1]
}
