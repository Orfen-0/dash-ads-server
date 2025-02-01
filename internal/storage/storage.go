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
	Bucket string
}

func NewMinIOStorage(cfg *config.MinIOConfig) (*MinIOStorage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, err
	}

	if !exists {
		err = client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{})
		if err != nil {
			return nil, err
		}
	}

	return &MinIOStorage{
		client: client,
		Bucket: cfg.Bucket,
	}, nil
}

func (s *MinIOStorage) UploadStream(ctx context.Context, objectName string, reader io.Reader) error {
	_, err := s.client.PutObject(ctx, s.Bucket, objectName, reader, -1, minio.PutObjectOptions{})
	if err != nil {
		log.Printf("Failed to upload object %s: %v", objectName, err)
		return err
	}
	log.Printf("Successfully uploaded object %s", objectName)
	return nil
}

func (s *MinIOStorage) DownloadStream(ctx context.Context, objectName string) (io.ReadCloser, error) {
	// Get the object from the bucket
	obj, err := s.client.GetObject(ctx, s.Bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		log.Printf("Failed to download object %s: %v", objectName, err)
		return nil, err
	}

	// Check if the object exists by reading its properties
	_, err = obj.Stat()
	if err != nil {
		log.Printf("Object %s does not exist or cannot be accessed: %v", objectName, err)
		return nil, err
	}

	log.Printf("Successfully opened object %s for streaming", objectName)
	return obj, nil
}

// Add more methods as needed, e.g., DownloadStream, DeleteObject, etc.
