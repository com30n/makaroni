package makaroni

import (
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/service/s3"
	"io"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	log "github.com/sirupsen/logrus"
)

// UploaderConfig contains settings for the uploader
type UploaderConfig struct {
	Endpoint            string
	DisableSSL          bool
	PathStyleAddressing bool
	Region              string
	Bucket              string
	KeyID               string
	Secret              string
	PartSize            int64         // Part size in bytes for multipart uploads
	Concurrency         int           // Number of concurrent uploads
	Timeout             time.Duration // Timeout for operations
}

// Uploader provides methods for uploading data to S3
type Uploader struct {
	uploader *s3manager.Uploader
	s3Client *s3.S3
	bucket   string
	config   UploaderConfig
}

// UploadFunc defines the function type for uploading content
type UploadFunc func(key string, content string, contentType string) error

// NewUploader creates a new uploader instance
func NewUploader(config UploaderConfig) (*Uploader, error) {
	log.Info("Creating AWS session")

	// Set default values if not specified
	if config.PartSize == 0 {
		config.PartSize = s3manager.DefaultUploadPartSize
	}
	if config.Concurrency == 0 {
		config.Concurrency = s3manager.DefaultUploadConcurrency
	}
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	awsSession, err := session.NewSession(&aws.Config{
		Credentials:      credentials.NewStaticCredentials(config.KeyID, config.Secret, ""),
		Endpoint:         &config.Endpoint,
		Region:           &config.Region,
		S3ForcePathStyle: &config.PathStyleAddressing,
		DisableSSL:       &config.DisableSSL,
	})
	if err != nil {
		log.Error("Failed to create AWS session: ", err)
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}
	log.Info("AWS session created successfully")

	// Create S3 client
	s3Client := s3.New(awsSession)
	log.Info("S3 client created successfully")

	uploader := s3manager.NewUploader(awsSession, func(u *s3manager.Uploader) {
		u.PartSize = config.PartSize
		u.Concurrency = config.Concurrency
	})
	log.Info("S3 uploader created successfully")

	return &Uploader{
		uploader: uploader,
		s3Client: s3Client,
		bucket:   config.Bucket,
		config:   config,
	}, nil
}

// UploadString uploads string content to S3
func (u *Uploader) UploadString(ctx context.Context, key string, content string, contentType string, metadata map[string]*string) error {
	return u.UploadReader(ctx, key, strings.NewReader(content), contentType, metadata)
}

// UploadReader uploads data from io.Reader to S3
func (u *Uploader) UploadReader(ctx context.Context, key string, reader io.Reader, contentType string, metadata map[string]*string) error {
	log.Debugf("Starting upload for key: %s", key)

	// Create a context with timeout if context is not set
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), u.config.Timeout)
		defer cancel()
	}

	input := &s3manager.UploadInput{
		Bucket:      &u.bucket,
		Key:         &key,
		Body:        reader,
		ContentType: &contentType,
	}

	if metadata != nil {
		input.Metadata = metadata
	}

	output, err := u.uploader.UploadWithContext(ctx, input)
	if err != nil {
		log.Errorf("Upload failed for key: %s, error: %v", key, err)
		return fmt.Errorf("upload failed for key %s: %w", key, err)
	}
	log.Debugf("Upload succeeded for key: %s, location: %s", key, output.Location)
	return nil
}

// GetMetadata retrieves metadata for an object
func (u *Uploader) GetMetadata(ctx context.Context, key string) (map[string]*string, error) {
	input := &s3.HeadObjectInput{
		Bucket: aws.String(u.bucket),
		Key:    aws.String(key),
	}

	result, err := u.s3Client.HeadObjectWithContext(ctx, input)
	if err != nil {
		log.Errorf("Error retrieving metadata for key: %s, error: %v", key, err)
		return nil, fmt.Errorf("error retrieving metadata for key %s: %w", key, err)
	}

	return result.Metadata, nil
}

// DeleteObjects removes multiple objects from storage in a single request
func (u *Uploader) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	log.Infof("Deleting %d objects in batch", len(keys))

	// Prepare list of objects to delete
	objects := make([]*s3.ObjectIdentifier, len(keys))
	for i, key := range keys {
		objects[i] = &s3.ObjectIdentifier{
			Key: aws.String(key),
		}
	}

	input := &s3.DeleteObjectsInput{
		Bucket: aws.String(u.bucket),
		Delete: &s3.Delete{
			Objects: objects,
			// Set Quiet to true to return only errors
			Quiet: aws.Bool(true),
		},
	}

	// Perform the batch deletion
	output, err := u.s3Client.DeleteObjectsWithContext(ctx, input)
	if err != nil && err.Error() != s3.ErrCodeNoSuchKey {
		log.Errorf("Error performing batch deletion: %v", err)
		return fmt.Errorf("error performing batch deletion: %w", err)
	}

	// Log errors for specific objects if any
	var outputErrors []*s3.Error
	if output != nil && len(output.Errors) > 0 {
		for _, e := range output.Errors {
			if *e.Message != s3.ErrCodeNoSuchKey {
				log.Warnf("Failed to delete object %s: %s", *e.Key, *e.Message)
				outputErrors = append(outputErrors, e)
			}
		}
		if len(outputErrors) > 0 {
			return fmt.Errorf("failed to delete %d objects", len(output.Errors))
		}
	}

	log.Debugf("Successfully deleted %d objects", len(keys))
	return nil
}
