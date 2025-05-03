package storage

import (
	"context"
	"github.com/Orfen-0/dash-ads-server/internal/config"
	"github.com/Orfen-0/dash-ads-server/internal/logging"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"io"
)

type MinIOStorage struct {
	client *minio.Client
	Bucket string
}

var logger = logging.New("objectstore")

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
		logger.Error("Failed to upload object", "objectName", objectName, "err", err)
		return err
	}
	logger.Info("Successfully uploaded object", "objectName", objectName)
	return nil
}

func (s *MinIOStorage) DownloadStream(ctx context.Context, objectName string) (io.ReadCloser, error) {
	// Get the object from the bucket
	obj, err := s.client.GetObject(ctx, s.Bucket, objectName, minio.GetObjectOptions{})
	if err != nil {
		logger.Error("Failed to upload object", "objectName", objectName, "err", err)
		return nil, err
	}

	// Check if the object exists by reading its properties
	_, err = obj.Stat()
	if err != nil {
		logger.Error("Object does not exist or cannot be accessed", "objectName", objectName, "err", err)
		return nil, err
	}

	logger.Info("Successfully opened object for streaming", "objectName", objectName)
	return obj, nil
}
