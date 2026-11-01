package transcription

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/transcribe"
	"github.com/aws/aws-sdk-go-v2/service/transcribe/types"
)

const (
	transcriptDownloadTimeout = 30 * time.Second
	maxTranscriptBytes        = 16 << 20
)

type TranscribeAPI interface {
	GetTranscriptionJob(context.Context, *transcribe.GetTranscriptionJobInput, ...func(*transcribe.Options)) (*transcribe.GetTranscriptionJobOutput, error)
}

// ManagedTranscriptReader gets a fresh signed URL on each invocation, including
// Lambda retries/redrives after the previous URL has expired. URLs are never logged.
type ManagedTranscriptReader struct {
	Client     TranscribeAPI
	HTTPClient *http.Client
}

var s3TranscriptHost = regexp.MustCompile(`^(?:[a-z0-9][a-z0-9.-]*\.)?s3(?:[.-][a-z0-9-]+)?\.amazonaws\.com(?:\.cn)?$`)

func (r ManagedTranscriptReader) Read(ctx context.Context, name string) ([]byte, error) {
	if r.Client == nil {
		return nil, errors.New("Transcribe client is required")
	}
	result, err := r.Client.GetTranscriptionJob(ctx, &transcribe.GetTranscriptionJobInput{TranscriptionJobName: aws.String(name)})
	if err != nil {
		return nil, operationError("Transcribe", err)
	}
	if result == nil || result.TranscriptionJob == nil {
		return nil, errors.New("missing Transcribe job")
	}
	job := result.TranscriptionJob
	if aws.ToString(job.TranscriptionJobName) != name || job.TranscriptionJobStatus != types.TranscriptionJobStatusCompleted || job.Transcript == nil {
		return nil, errors.New("incomplete or mismatched Transcribe job")
	}
	location, err := url.Parse(aws.ToString(job.Transcript.TranscriptFileUri))
	if err != nil || location.Scheme != "https" || location.User != nil || location.Port() != "" || location.Fragment != "" || !s3TranscriptHost.MatchString(location.Hostname()) {
		return nil, errors.New("invalid Transcribe transcript location")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, errors.New("invalid transcript download request")
	}
	client := http.Client{Timeout: transcriptDownloadTimeout}
	if r.HTTPClient != nil {
		client = *r.HTTPClient
		client.Timeout = transcriptDownloadTimeout
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		// url.Error includes the signed query string; expose only the underlying error.
		var requestError *url.Error
		if errors.As(err, &requestError) {
			err = requestError.Err
		}
		return nil, operationError("transcript download", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		err := fmt.Errorf("transcript download returned HTTP %d", response.StatusCode)
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			return nil, &TransientError{Err: err}
		}
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxTranscriptBytes+1))
	if err != nil {
		return nil, operationError("transcript download", err)
	}
	if len(data) > maxTranscriptBytes {
		return nil, errors.New("provider transcript exceeds download limit")
	}
	return data, nil
}
