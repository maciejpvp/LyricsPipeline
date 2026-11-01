package transcription

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf16"
)

type inputEvent struct {
	Source     string `json:"source"`
	DetailType string `json:"detail-type"`
	Detail     *struct {
		Bucket struct {
			Name string `json:"name"`
		} `json:"bucket"`
		Object struct {
			Key  string `json:"key"`
			ETag string `json:"etag"`
		} `json:"object"`
	} `json:"detail"`
	Bucket  string  `json:"bucket"`
	Key     string  `json:"key"`
	ETag    string  `json:"etag"`
	Attempt *string `json:"attempt"`
}

var attemptPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

func (h Handler) Prepare(ctx context.Context, input json.RawMessage) (Job, error) {
	var event inputEvent
	if err := json.Unmarshal(input, &event); err != nil {
		return Job{}, fmt.Errorf("invalid event: %w", err)
	}
	attempt := "default"
	if event.Detail != nil {
		if event.Source != "aws.s3" || event.DetailType != "Object Created" {
			return Job{}, errors.New("expected S3 Object Created event")
		}
		event.Bucket, event.Key, event.ETag = event.Detail.Bucket.Name, event.Detail.Object.Key, event.Detail.Object.ETag
	} else if event.Attempt != nil {
		attempt = *event.Attempt
	}
	prefix := h.Settings.OutputPrefix + "/"
	if event.Bucket != h.Settings.MediaBucket || !strings.HasPrefix(event.Key, prefix) {
		return Job{}, errors.New("source is outside the configured bucket/prefix")
	}
	pieces := strings.Split(strings.TrimPrefix(event.Key, prefix), "/")
	if len(pieces) != 2 || pieces[0] == "" || pieces[0] == "." || pieces[0] == ".." || pieces[1] != "vocals.wav" {
		return Job{}, errors.New("expected outputPrefix/job_id/vocals.wav")
	}
	if !attemptPattern.MatchString(attempt) {
		return Job{}, errors.New("invalid attempt identifier")
	}
	source, err := h.Store.Source(ctx, event.Bucket, event.Key)
	if err != nil {
		return Job{}, err
	}
	if event.ETag != "" && strings.Trim(event.ETag, `"`) != source.ETag {
		return Job{}, errors.New("stale event: vocals.wav was replaced")
	}
	jobID := pieces[0]
	root := prefix + jobID + "/transcription/"
	pinned, err := createDocument(ctx, h.Store, event.Bucket, root+"source.json", source)
	if err != nil {
		return Job{}, err
	}
	if pinned != source {
		return Job{}, errors.New("job_id is immutable: vocals.wav content has changed")
	}
	existing, found, err := readDocument[Transcript](ctx, h.Store, event.Bucket, root+"transcript.json")
	if err != nil {
		return Job{}, err
	}
	if found {
		if existing.Source != source || existing.SchemaVersion != 1 || existing.PipelineVersion != h.Settings.PipelineVersion {
			return Job{}, errors.New("existing transcript belongs to a different source or pipeline version; use a new job_id")
		}
		return Job{Cached: true, Bucket: event.Bucket, TranscriptKey: root + "transcript.json"}, nil
	}
	hash := sha256.Sum256([]byte(identityJSON(h.Settings.AccountID, attempt, h.Settings.PipelineVersion, source)))
	runID := hex.EncodeToString(hash[:])
	return Job{Bucket: event.Bucket, JobID: jobID, Source: &source, RunID: runID, JobName: "lyrics-" + runID,
		MediaURI: "s3://" + event.Bucket + "/" + event.Key, RawKey: root + "runs/" + runID + "/raw.json", TranscriptKey: root + "transcript.json", PipelineVersion: h.Settings.PipelineVersion}, nil
}

func (h Handler) Finalize(ctx context.Context, job Job) (Result, error) {
	if job.Source == nil || job.Bucket != h.Settings.MediaBucket || job.Source.Bucket != job.Bucket || job.RawKey == "" || job.TranscriptKey == "" || job.JobName == "" || job.PipelineVersion != h.Settings.PipelineVersion {
		return Result{}, errors.New("invalid finalization job")
	}
	source, err := h.Store.Source(ctx, job.Bucket, job.Source.Key)
	if err != nil {
		return Result{}, err
	}
	if source != *job.Source {
		return Result{}, errors.New("vocals.wav changed during transcription")
	}
	raw, found, err := readDocument[RawTranscript](ctx, h.Store, job.Bucket, job.RawKey)
	if err != nil {
		return Result{}, err
	}
	var downloaded []byte
	if !found {
		if h.Provider == nil {
			return Result{}, errors.New("transcript provider is required for missing raw output")
		}
		data, err := h.Provider.Read(ctx, job.JobName)
		if err != nil {
			return Result{}, err
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return Result{}, fmt.Errorf("invalid provider JSON: %w", err)
		}
		downloaded = data
	}
	if raw.JobName != job.JobName || raw.Status != "COMPLETED" {
		return Result{}, errors.New("missing or mismatched Transcribe result")
	}
	document, err := Normalize(raw, job, h.Settings.Language)
	if err != nil {
		return Result{}, err
	}
	if !found {
		saved, err := h.Store.Create(ctx, job.Bucket, job.RawKey, downloaded)
		if err != nil {
			return Result{}, err
		}
		if !bytes.Equal(saved, downloaded) {
			var existing, expected any
			if err := json.Unmarshal(saved, &existing); err != nil {
				return Result{}, fmt.Errorf("invalid saved provider JSON: %w", err)
			}
			if err := json.Unmarshal(downloaded, &expected); err != nil {
				return Result{}, err
			}
			if !reflect.DeepEqual(existing, expected) {
				return Result{}, errors.New("a different raw transcript already exists")
			}
		}
	}
	saved, err := createDocument(ctx, h.Store, job.Bucket, job.TranscriptKey, document)
	if err != nil {
		return Result{}, err
	}
	if !reflect.DeepEqual(saved, document) {
		return Result{}, errors.New("a different transcript already exists; use a new job_id")
	}
	slog.Info("transcription_completed", "job_id", job.JobID, "run_id", job.RunID, "words", len(document.Words))
	return Result{Bucket: job.Bucket, TranscriptKey: job.TranscriptKey, WordCount: len(document.Words)}, nil
}

// Preserve the exact identity encoding of the previous implementation, including
// spaces, sorted keys and ASCII Unicode escapes. In-flight tasks keep their job names.
func identityJSON(account, attempt, version string, source Source) string {
	return `{"account": ` + asciiJSON(account) + `, "attempt": ` + asciiJSON(attempt) + `, "pipeline_version": ` + asciiJSON(version) + `, "source": {"bucket": ` + asciiJSON(source.Bucket) + `, "etag": ` + asciiJSON(source.ETag) + `, "key": ` + asciiJSON(source.Key) + `}}`
}
func asciiJSON(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r >= 0x20 && r < 0x7f {
				b.WriteRune(r)
			} else if r <= 0xffff {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				a, c := utf16.EncodeRune(r)
				fmt.Fprintf(&b, `\u%04x\u%04x`, a, c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
