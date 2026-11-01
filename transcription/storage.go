package transcription

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type S3API interface {
	HeadObject(context.Context, *s3.HeadObjectInput, ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}
type S3Store struct{ Client S3API }

func (s S3Store) Source(ctx context.Context, bucket, key string) (Source, error) {
	head, err := s.Client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return Source{}, storageError(err)
	}
	if aws.ToInt64(head.ContentLength) <= 0 || aws.ToString(head.ETag) == "" {
		return Source{}, errors.New("empty vocals.wav or missing ETag")
	}
	return Source{Bucket: bucket, Key: key, ETag: strings.Trim(aws.ToString(head.ETag), `"`)}, nil
}
func (s S3Store) Read(ctx context.Context, bucket, key string) ([]byte, error) {
	// Exact-key listing distinguishes absence from 403 without unrestricted ListBucket.
	listing, err := s.Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(bucket), Prefix: aws.String(key), MaxKeys: aws.Int32(1)})
	if err != nil {
		return nil, storageError(err)
	}
	found := false
	for _, object := range listing.Contents {
		if aws.ToString(object.Key) == key {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	result, err := s.Client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		if errorCode(err) == "NoSuchKey" || statusCode(err) == 404 {
			return nil, ErrNotFound
		}
		return nil, storageError(err)
	}
	defer result.Body.Close()
	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, storageError(err)
	}
	return data, nil
}
func (s S3Store) Create(ctx context.Context, bucket, key string, data []byte) ([]byte, error) {
	_, err := s.Client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(data), ContentType: aws.String("application/json"), IfNoneMatch: aws.String("*")})
	if err == nil {
		return data, nil
	}
	if errorCode(err) != "PreconditionFailed" && statusCode(err) != 412 {
		return nil, storageError(err)
	}
	saved, err := s.Read(ctx, bucket, key)
	if errors.Is(err, ErrNotFound) {
		return nil, &TransientError{Err: errors.New("conditional write conflicted but object is absent")}
	}
	return saved, err
}
func errorCode(err error) string {
	var api smithy.APIError
	if errors.As(err, &api) {
		return api.ErrorCode()
	}
	return ""
}
func statusCode(err error) int {
	var response *smithyhttp.ResponseError
	if errors.As(err, &response) {
		return response.HTTPStatusCode()
	}
	return 0
}
func storageError(err error) error {
	return operationError("S3", err)
}

func operationError(service string, err error) error {
	retry := false
	switch errorCode(err) {
	case "SlowDown", "ServiceUnavailable", "InternalError", "RequestTimeout", "RequestTimeoutException", "ConditionalRequestConflict", "InternalFailureException", "LimitExceededException", "ThrottlingException":
		retry = true
	}
	status := statusCode(err)
	if status == 408 || status == 409 || status == 429 || status >= 500 {
		retry = true
	}
	var network net.Error
	if errors.As(err, &network) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, context.DeadlineExceeded) {
		retry = true
	}
	if retry {
		return &TransientError{Err: fmt.Errorf("temporary %s failure: %w", service, err)}
	}
	return fmt.Errorf("%s operation failed: %w", service, err)
}
