package transcription

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/transcribe"
	"github.com/aws/aws-sdk-go-v2/service/transcribe/types"
)

type transcribeStub func(context.Context, *transcribe.GetTranscriptionJobInput, ...func(*transcribe.Options)) (*transcribe.GetTranscriptionJobOutput, error)

func (f transcribeStub) GetTranscriptionJob(ctx context.Context, in *transcribe.GetTranscriptionJobInput, options ...func(*transcribe.Options)) (*transcribe.GetTranscriptionJobOutput, error) {
	return f(ctx, in, options...)
}

func providerResult(uri string) *transcribe.GetTranscriptionJobOutput {
	return &transcribe.GetTranscriptionJobOutput{TranscriptionJob: &types.TranscriptionJob{
		TranscriptionJobName: aws.String("lyrics-test"), TranscriptionJobStatus: types.TranscriptionJobStatusCompleted,
		Transcript: &types.Transcript{TranscriptFileUri: aws.String(uri)},
	}}
}

func TestManagedTranscriptRefreshesURLOnRetry(t *testing.T) {
	lookups, downloads := 0, 0
	reader := ManagedTranscriptReader{
		Client: transcribeStub(func(_ context.Context, in *transcribe.GetTranscriptionJobInput, _ ...func(*transcribe.Options)) (*transcribe.GetTranscriptionJobOutput, error) {
			if aws.ToString(in.TranscriptionJobName) != "lyrics-test" {
				t.Fatal("looked up the wrong job")
			}
			lookups++
			if lookups == 1 {
				return providerResult("https://s3.eu-central-1.amazonaws.com/provider/raw.json?token=expired"), nil
			}
			return providerResult("https://s3.eu-central-1.amazonaws.com/provider/raw.json?token=fresh"), nil
		}),
		HTTPClient: &http.Client{Transport: roundTrip(func(req *http.Request) (*http.Response, error) {
			downloads++
			if req.Method != http.MethodGet || req.Header.Get("Authorization") != "" {
				t.Fatal("signed URL must be fetched using GET without Lambda AWS credentials")
			}
			if req.URL.Query().Get("token") == "expired" {
				return response(403, "expired"), nil
			}
			return response(200, `{"jobName":"lyrics-test"}`), nil
		})},
	}
	_, err := reader.Read(t.Context(), "lyrics-test")
	var transient *TransientError
	if !errors.As(err, &transient) || strings.Contains(err.Error(), "token=") {
		t.Fatalf("expired URL must retry without exposing the URL: %v", err)
	}
	data, err := reader.Read(t.Context(), "lyrics-test")
	if err != nil || string(data) != `{"jobName":"lyrics-test"}` || lookups != 2 || downloads != 2 {
		t.Fatalf("retry did not refresh signed URL: %s %v lookups=%d downloads=%d", data, err, lookups, downloads)
	}
}

func TestManagedTranscriptRejectsInvalidLocationsAndJobs(t *testing.T) {
	for _, uri := range []string{
		"", "http://s3.amazonaws.com/provider/result.json", "https://example.com/result.json",
		"https://s3.amazonaws.com.evil.example/result.json", "https://localhost/result.json",
		"https://user:secret@s3.amazonaws.com/result.json", "https://s3.amazonaws.com:8443/result.json",
	} {
		t.Run(uri, func(t *testing.T) {
			reader := ManagedTranscriptReader{Client: transcribeStub(func(context.Context, *transcribe.GetTranscriptionJobInput, ...func(*transcribe.Options)) (*transcribe.GetTranscriptionJobOutput, error) {
				return providerResult(uri), nil
			}), HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid location was fetched")
				return nil, nil
			})}}
			if _, err := reader.Read(t.Context(), "lyrics-test"); err == nil {
				t.Fatal("invalid provider location accepted")
			}
		})
	}
	for _, result := range []*transcribe.GetTranscriptionJobOutput{
		nil, {}, {TranscriptionJob: &types.TranscriptionJob{TranscriptionJobName: aws.String("other-job")}},
		{TranscriptionJob: &types.TranscriptionJob{TranscriptionJobName: aws.String("lyrics-test"), TranscriptionJobStatus: types.TranscriptionJobStatusInProgress}},
	} {
		reader := ManagedTranscriptReader{Client: transcribeStub(func(context.Context, *transcribe.GetTranscriptionJobInput, ...func(*transcribe.Options)) (*transcribe.GetTranscriptionJobOutput, error) {
			return result, nil
		})}
		if _, err := reader.Read(t.Context(), "lyrics-test"); err == nil {
			t.Fatal("missing, incomplete or mismatched job accepted")
		}
	}
}

func TestManagedTranscriptDownloadFailures(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		body      io.ReadCloser
		err       error
		transient bool
	}{
		{"redirect", 302, io.NopCloser(strings.NewReader("")), nil, false},
		{"not found", 404, io.NopCloser(strings.NewReader("")), nil, false},
		{"throttled", 429, io.NopCloser(strings.NewReader("")), nil, true},
		{"server failure", 503, io.NopCloser(strings.NewReader("")), nil, true},
		{"network timeout", 0, nil, context.DeadlineExceeded, true},
		{"oversize", 200, io.NopCloser(io.LimitReader(repeatingByte{}, maxTranscriptBytes+1)), nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			reader := ManagedTranscriptReader{Client: transcribeStub(func(context.Context, *transcribe.GetTranscriptionJobInput, ...func(*transcribe.Options)) (*transcribe.GetTranscriptionJobOutput, error) {
				return providerResult("https://provider.s3.eu-central-1.amazonaws.com/result.json?token=secret"), nil
			}), HTTPClient: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				if test.err != nil {
					return nil, test.err
				}
				return &http.Response{StatusCode: test.status, Body: test.body, Header: http.Header{"Location": []string{"https://example.com/"}}}, nil
			})}}
			_, err := reader.Read(t.Context(), "lyrics-test")
			var transient *TransientError
			if err == nil || errors.As(err, &transient) != test.transient || strings.Contains(err.Error(), "token=secret") || calls != 1 {
				t.Fatalf("unexpected download error/retry/redirect: %v calls=%d", err, calls)
			}
		})
	}
}

type repeatingByte struct{}

func (repeatingByte) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
