// Package storage provides MinIO/S3-compatible object storage helpers.
package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Storage wraps a MinIO client for a single bucket.
type Storage struct {
	client *minio.Client
	bucket string
}

// New creates a Storage client and ensures the bucket exists.
func New(ctx context.Context, endpoint, accessKey, secretKey string, useSSL bool, bucket string) (*Storage, error) {
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	s := &Storage{client: client, bucket: bucket}
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}
	return s, nil
}

// Bucket returns the configured bucket name.
func (s *Storage) Bucket() string { return s.bucket }

// PresignPut returns a short-lived presigned PUT URL for key.
func (s *Storage) PresignPut(ctx context.Context, key string, contentType string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedPutObject(ctx, s.bucket, key, expiry)
	if err != nil {
		return "", fmt.Errorf("presign put %s: %w", key, err)
	}
	return url.String(), nil
}

// PresignGet returns a short-lived presigned GET URL for key.
func (s *Storage) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	url, err := s.client.PresignedGetObject(ctx, s.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign get %s: %w", key, err)
	}
	return url.String(), nil
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

// Stat returns metadata for key.
func (s *Storage) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	obj, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Key: obj.Key, Size: obj.Size, ETag: obj.ETag, LastModified: obj.LastModified}, nil
}

// Get opens a reader for key.
func (s *Storage) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	info, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, ObjectInfo{}, err
	}
	return obj, ObjectInfo{Key: info.Key, Size: info.Size, ETag: info.ETag}, nil
}

// Delete removes an object (no error if missing).
func (s *Storage) Delete(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		// Treat missing objects as success for idempotent cleanup.
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil
		}
		return fmt.Errorf("delete %s: %w", key, err)
	}
	return nil
}

// PutBytes stores raw bytes with a content type.
func (s *Storage) PutBytes(ctx context.Context, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytesReader(data), int64(len(data)), minio.PutObjectOptions{ContentType: contentType})
	return err
}
