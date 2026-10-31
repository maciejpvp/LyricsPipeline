// Package transcription implements the Go Lambda boundaries around Amazon Transcribe.
// It handles metadata and transcript JSON only; audio processing stays in the managed service.
package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

type Settings struct{ MediaBucket, OutputPrefix, AccountID, PipelineVersion, Language, Action string }

func SettingsFromEnv() (Settings, error) {
	s := Settings{MediaBucket: os.Getenv("MEDIA_BUCKET"), OutputPrefix: os.Getenv("OUTPUT_PREFIX"), AccountID: os.Getenv("ACCOUNT_ID"), PipelineVersion: os.Getenv("PIPELINE_VERSION"), Language: os.Getenv("LANGUAGE"), Action: os.Getenv("TRANSCRIPTION_ACTION")}
	return s, s.Validate()
}
func (s Settings) Validate() error {
	if s.MediaBucket == "" || s.OutputPrefix == "" || s.AccountID == "" || s.PipelineVersion == "" || s.Language == "" {
		return errors.New("MEDIA_BUCKET, OUTPUT_PREFIX, ACCOUNT_ID, PIPELINE_VERSION and LANGUAGE are required")
	}
	if s.Action != "prepare" && s.Action != "finalize" {
		return errors.New("TRANSCRIPTION_ACTION must be prepare or finalize")
	}
	return nil
}

type Source struct {
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
	ETag   string `json:"etag"`
}
type Job struct {
	Cached          bool    `json:"cached"`
	Bucket          string  `json:"bucket"`
	JobID           string  `json:"job_id,omitempty"`
	Source          *Source `json:"source,omitempty"`
	RunID           string  `json:"run_id,omitempty"`
	JobName         string  `json:"job_name,omitempty"`
	MediaURI        string  `json:"media_uri,omitempty"`
	RawKey          string  `json:"raw_key,omitempty"`
	TranscriptKey   string  `json:"transcript_key"`
	PipelineVersion string  `json:"pipeline_version,omitempty"`
}
type Word struct {
	Text       string   `json:"text"`
	Start      float64  `json:"start"`
	End        float64  `json:"end"`
	Confidence *float64 `json:"confidence"`
}
type Transcript struct {
	SchemaVersion   int    `json:"schema_version"`
	PipelineVersion string `json:"pipeline_version"`
	JobID           string `json:"job_id"`
	Language        string `json:"language"`
	Source          Source `json:"source"`
	Text            string `json:"text"`
	Words           []Word `json:"words"`
}
type Result struct {
	Bucket        string `json:"bucket"`
	TranscriptKey string `json:"transcript_key"`
	WordCount     int    `json:"word_count"`
}
type RawTranscript struct {
	JobName string `json:"jobName"`
	Status  string `json:"status"`
	Results struct {
		Transcripts []struct {
			Transcript *string `json:"transcript"`
		} `json:"transcripts"`
		Items []RawItem `json:"items"`
	} `json:"results"`
}
type RawItem struct {
	Type         string          `json:"type"`
	Start        json.RawMessage `json:"start_time"`
	End          json.RawMessage `json:"end_time"`
	Alternatives []struct {
		Content    string          `json:"content"`
		Confidence json.RawMessage `json:"confidence"`
	} `json:"alternatives"`
}

var ErrNotFound = errors.New("object not found")

type ObjectStore interface {
	Source(context.Context, string, string) (Source, error)
	Read(context.Context, string, string) ([]byte, error)
	// Create returns the saved document, including the winning concurrent write.
	Create(context.Context, string, string, []byte) ([]byte, error)
}

// TransientError is serialized as this errorType by aws-lambda-go. The workflow
// retries it; invalid input and permanent authorization errors are not retried.
type TransientError struct{ Err error }

func (e *TransientError) Error() string { return e.Err.Error() }
func (e *TransientError) Unwrap() error { return e.Err }

type Handler struct {
	Store    ObjectStore
	Settings Settings
}

func (h Handler) Handle(ctx context.Context, input json.RawMessage) (any, error) {
	if err := h.Settings.Validate(); err != nil {
		return nil, err
	}
	if h.Store == nil {
		return nil, errors.New("object store is required")
	}
	if h.Settings.Action == "prepare" {
		return h.Prepare(ctx, input)
	}
	var job Job
	if err := json.Unmarshal(input, &job); err != nil {
		return nil, fmt.Errorf("invalid finalization input: %w", err)
	}
	return h.Finalize(ctx, job)
}

func readDocument[T any](ctx context.Context, store ObjectStore, bucket, key string) (T, bool, error) {
	var result T
	data, err := store.Read(ctx, bucket, key)
	if errors.Is(err, ErrNotFound) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return result, false, fmt.Errorf("invalid JSON artifact: %w", err)
	}
	return result, true, nil
}
func createDocument[T any](ctx context.Context, store ObjectStore, bucket, key string, document T) (T, error) {
	var result T
	data, err := json.Marshal(document)
	if err != nil {
		return result, err
	}
	saved, err := store.Create(ctx, bucket, key, data)
	if err != nil {
		return result, err
	}
	if err = json.Unmarshal(saved, &result); err != nil {
		return result, fmt.Errorf("invalid saved artifact: %w", err)
	}
	return result, nil
}
