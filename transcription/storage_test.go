package transcription

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testS3(transport roundTrip) S3Store {
	return S3Store{Client: s3.New(s3.Options{Region: "eu-central-1", BaseEndpoint: aws.String("https://s3.example"), UsePathStyle: true,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
		}),
		Retryer: aws.NopRetryer{}, HTTPClient: &http.Client{Transport: transport},
	})}
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func TestS3ConditionalCreateUsesWinningJSON(t *testing.T) {
	const saved = `{"saved":true}`
	calls := 0
	store := testS3(func(req *http.Request) (*http.Response, error) {
		calls++
		switch req.Method {
		case "PUT":
			if req.Header.Get("If-None-Match") != "*" || req.Header.Get("Content-Type") != "application/json" {
				t.Fatal("conditional JSON headers missing")
			}
			return response(412, `<Error><Code>PreconditionFailed</Code><Message>exists</Message></Error>`), nil
		case "GET":
			if req.URL.Query().Get("list-type") == "2" {
				if req.URL.Query().Get("prefix") != "output/song/transcription/source.json" || req.URL.Query().Get("max-keys") != "1" {
					t.Fatal("ListBucket not scoped to exact key")
				}
				return response(200, `<ListBucketResult><Contents><Key>output/song/transcription/source.json</Key></Contents></ListBucketResult>`), nil
			}
			return response(200, saved), nil
		default:
			t.Fatalf("unexpected %s", req.Method)
			return nil, nil
		}
	})
	result, err := store.Create(context.Background(), "media", "output/song/transcription/source.json", []byte(`{"other":true}`))
	if err != nil || string(result) != saved || calls != 3 {
		t.Fatalf("lost concurrent write: %s %v calls=%d", result, err, calls)
	}
}
func TestS3MissingKeyDoesNotHideAccessDenied(t *testing.T) {
	for _, status := range []int{200, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			store := testS3(func(req *http.Request) (*http.Response, error) {
				if status == 200 {
					return response(200, `<ListBucketResult/>`), nil
				}
				return response(403, `<Error><Code>AccessDenied</Code><Message>denied</Message></Error>`), nil
			})
			_, err := store.Read(context.Background(), "media", "output/song/transcription/transcript.json")
			if status == 200 && !errors.Is(err, ErrNotFound) {
				t.Fatalf("missing object: %v", err)
			}
			if status == 403 && (err == nil || errors.Is(err, ErrNotFound)) {
				t.Fatal("authorization failure treated as absence")
			}
		})
	}
}
func TestS3TransientClassification(t *testing.T) {
	for _, status := range []int{403, 409, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			store := testS3(func(*http.Request) (*http.Response, error) {
				return response(status, `<Error><Code>TestError</Code><Message>failure</Message></Error>`), nil
			})
			_, err := store.Create(context.Background(), "media", "output/song/transcription/source.json", []byte(`{}`))
			var transient *TransientError
			retry := errors.As(err, &transient)
			if err == nil || retry != (status != 403) {
				t.Fatalf("wrong retry classification: %v", err)
			}
		})
	}
}
func TestS3SourceAndEmptyAudio(t *testing.T) {
	for _, size := range []string{"123", "0"} {
		t.Run(size, func(t *testing.T) {
			store := testS3(func(req *http.Request) (*http.Response, error) {
				if req.Method != "HEAD" {
					t.Fatal("downloaded audio instead of reading metadata")
				}
				out := response(200, "")
				out.Header.Set("ETag", `"audio-etag"`)
				out.Header.Set("Content-Length", size)
				return out, nil
			})
			source, err := store.Source(context.Background(), "media", "output/song/vocals.wav")
			if size == "0" {
				if err == nil {
					t.Fatal("empty audio accepted")
				}
				return
			}
			if err != nil || source.ETag != "audio-etag" {
				t.Fatalf("wrong source: %+v %v", source, err)
			}
		})
	}
}
