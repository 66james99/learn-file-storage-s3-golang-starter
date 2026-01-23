package main

import (
	"context"
	"time"
	"io"
	"mime"
	"net/http"
	"os"
	"fmt"
	"strings"


	"github.com/66james99/learn-file-storage-s3-golang-starter/internal/auth"
	"github.com/66james99/learn-file-storage-s3-golang-starter/internal/database"
	"github.com/google/uuid"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func (cfg *apiConfig) handlerUploadVideo(w http.ResponseWriter, r *http.Request) {
	videoIDString := r.PathValue("videoID")
	videoID, err := uuid.Parse(videoIDString)
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid ID", err)
		return
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't find JWT", err)
		return
	}

	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		respondWithError(w, http.StatusUnauthorized, "Couldn't validate JWT", err)
		return
	}

		video, err := cfg.db.GetVideo(videoID)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't find video", err)
		return
	}
	if video.UserID != userID {
		respondWithError(w, http.StatusUnauthorized, "Not authorized to upload this video", nil)
		return
	}

	const maxMemory = 10 << 30 // 1 GB
	r.ParseMultipartForm(maxMemory)

	file, header, err := r.FormFile("video")
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Unable to parse form file", err)
		return
	}
	defer file.Close()

	mediaType, _, err := mime.ParseMediaType(header.Header.Get("Content-Type"))
	if err != nil {
		respondWithError(w, http.StatusBadRequest, "Invalid Content-Type", err)
		return
	}
	if mediaType != "video/mp4" {
		respondWithError(w, http.StatusBadRequest, "Invalid file type", nil)
		return
	}

	assetPath := getAssetPath(mediaType)

	tmpFile, err := os.CreateTemp("", assetPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Unable to create temp file on server", err)
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	_, err = io.Copy(tmpFile, file)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error saving file", err)
		return
	}

	

	aspectRatio, err := getVideoAspectRatio(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error getting video aspect ratio", err)
		return
	}

	ret, err := tmpFile.Seek(0, io.SeekStart)
	if err != nil || ret != 0 {
		respondWithError(w, http.StatusInternalServerError, "Error seeking to beginning of file", err)
		return
	}

	fastStartPath, err := processVideoForFastStart(tmpFile.Name())
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error processing video for fast start", err)
		return
	}

	fastStartFile, err := os.Open(fastStartPath)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error opening fast start file", err)
		return
	}
	defer os.Remove(fastStartPath)
	defer fastStartFile.Close()
	

	var s3key string
	switch aspectRatio {
	case "16:9":
		s3key = fmt.Sprintf("landscape/%s", assetPath)
	case "9:16":
		s3key = fmt.Sprintf("portrait/%s", assetPath)
	default:
		s3key = fmt.Sprintf("other/%s", assetPath)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err = cfg.s3client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(cfg.s3Bucket),
		Key:         aws.String(s3key),
		Body:        fastStartFile,
		ContentType: aws.String(mediaType),
		})
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Error uploading file to S3", err)
		return
	}

	// videoURL := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", cfg.s3Bucket, cfg.s3Region, s3key)
	videoURL := fmt.Sprintf("%s,%s", cfg.s3Bucket, s3key)
	video.VideoURL = &videoURL
	err = cfg.db.UpdateVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't update video", err)
		return
	}

	signedVideo, err := cfg.dbVideoToSignedVideo(video)
	if err != nil {
		respondWithError(w, http.StatusInternalServerError, "Couldn't generate signed video URL", err)
		return
	}

	respondWithJSON(w, http.StatusOK, signedVideo)
}

func (cfg *apiConfig) dbVideoToSignedVideo(video database.Video) (database.Video, error) {
	if video.VideoURL == nil {
		return video, nil
	}
	parts := strings.Split(*video.VideoURL, ",")
	if len(parts) < 2 {
		return video, nil
	}
	bucket := parts[0]
	key := parts[1]
	presigned, err := generatePresignedURL(cfg.s3client, bucket, key, 5*time.Minute)
	if err != nil {
		return video, err
	}
	video.VideoURL = &presigned
	return video, nil
}

func generatePresignedURL(s3Client *s3.Client, bucket, key string, expireTime time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s3Client)
	presignedUrl, err := presignClient.PresignGetObject(context.TODO(), &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expireTime))
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %v", err)
	}
	return presignedUrl.URL, nil
}