package storage

import (
	"context"
	"io"
	"log"

	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOStorage struct {
	client *minio.Client
	bucket string
}

func NewMinIOStorage(cfg *config.MinIOConfig) (*MinIOStorage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	return &MinIOStorage{
		client: client,
		bucket: cfg.Bucket,
	}, nil
}

func (s *MinIOStorage) UploadStream(ctx context.Context, objectName string, reader io.Reader) error {
	_, err := s.client.PutObject(ctx, s.bucket, objectName, reader, -1, minio.PutObjectOptions{})
	if err != nil {
		log.Printf("Failed to upload object %s: %v", objectName, err)
		return err
	}
	log.Printf("Successfully uploaded object %s", objectName)
	return nil
}

// Add more methods as needed, e.g., DownloadStream, DeleteObject, etc.
