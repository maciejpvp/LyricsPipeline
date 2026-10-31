package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

var testSettings = Settings{MediaBucket: "media", OutputPrefix: "output", AccountID: "123456789012", PipelineVersion: "1", Language: "en-US", Action: "prepare"}
var testEvent = json.RawMessage(`{"bucket":"media","key":"output/song/vocals.wav"}`)

type memoryStore struct {
	sync.Mutex
	objects map[string][]byte
	writes  map[string]int
	etag    string
	failure error
}

func newStore() *memoryStore {
	return &memoryStore{objects: map[string][]byte{}, writes: map[string]int{}, etag: "audio-etag"}
}
func (s *memoryStore) Source(_ context.Context, bucket, key string) (Source, error) {
	s.Lock()
	defer s.Unlock()
	return Source{bucket, key, s.etag}, s.failure
}
func (s *memoryStore) Read(_ context.Context, bucket, key string) ([]byte, error) {
	s.Lock()
	defer s.Unlock()
	if s.failure != nil {
		return nil, s.failure
	}
	data, ok := s.objects[key]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), data...), nil
}
func (s *memoryStore) Create(_ context.Context, bucket, key string, data []byte) ([]byte, error) {
	s.Lock()
	defer s.Unlock()
	if s.failure != nil {
		return nil, s.failure
	}
	if saved, ok := s.objects[key]; ok {
		return saved, nil
	}
	s.objects[key] = data
	s.writes[key]++
	return data, nil
}
func prepareTest(t *testing.T) (Handler, *memoryStore, Job) {
	t.Helper()
	store := newStore()
	h := Handler{store, testSettings}
	job, err := h.Prepare(context.Background(), testEvent)
	if err != nil {
		t.Fatal(err)
	}
	return h, store, job
}
func rawFixture(t *testing.T, job Job) RawTranscript {
	t.Helper()
	var raw RawTranscript
	document := `{"status":"COMPLETED","results":{"transcripts":[{"transcript":"Hello world!"}],"items":[{"type":"pronunciation","start_time":"1.25","end_time":"1.7","alternatives":[{"content":"Hello","confidence":"0.98"}]},{"type":"pronunciation","start_time":"1.8","end_time":"2.4","alternatives":[{"content":"world","confidence":"0.97"}]},{"type":"punctuation","alternatives":[{"content":"!"}]}]}}`
	if err := json.Unmarshal([]byte(document), &raw); err != nil {
		t.Fatal(err)
	}
	raw.JobName = job.JobName
	return raw
}
func saveRaw(t *testing.T, store *memoryStore, job Job, raw RawTranscript) {
	t.Helper()
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	store.objects[job.RawKey] = data
}

func TestDuplicateAndConcurrentPublication(t *testing.T) {
	h, store, job := prepareTest(t)
	duplicate, err := h.Prepare(context.Background(), testEvent)
	if err != nil || !reflect.DeepEqual(job, duplicate) {
		t.Fatalf("duplicate job changed: %v", err)
	}
	saveRaw(t, store, job, rawFixture(t, job))
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		group.Go(func() { _, err := h.Finalize(context.Background(), job); failures <- err })
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	transcript, found, err := readDocument[Transcript](context.Background(), store, "media", job.TranscriptKey)
	if err != nil || !found {
		t.Fatal(err)
	}
	if len(transcript.Words) != 2 || transcript.Words[1].Text != "world!" || transcript.Words[0].Start != 1.25 || *transcript.Words[0].Confidence != 0.98 {
		t.Fatalf("wrong words: %+v", transcript.Words)
	}
	if store.writes[job.TranscriptKey] != 1 {
		t.Fatal("duplicate replaced final transcript")
	}
	cached, err := h.Prepare(context.Background(), testEvent)
	if err != nil || !cached.Cached {
		t.Fatalf("completed job not reused: %v", err)
	}
}
func TestRunIdentityCompatibility(t *testing.T) {
	for _, tc := range []struct{ key, want string }{
		{"output/song/vocals.wav", "f2af8e8a60e41f76c528b50bcaf2fb11047913d8c66c221e090ac7f4e5de82c3"},
		{"output/piosenka-ą🎵/vocals.wav", "acc66f6502dc278f89b7fde809ebf008ef5f5210816aa57bd70a0e681af08e0b"},
	} {
		t.Run(tc.key, func(t *testing.T) {
			event, _ := json.Marshal(struct {
				Bucket string `json:"bucket"`
				Key    string `json:"key"`
			}{"media", tc.key})
			h := Handler{newStore(), testSettings}
			job, err := h.Prepare(context.Background(), event)
			if err != nil {
				t.Fatal(err)
			}
			if job.RunID != tc.want {
				t.Fatalf("previous provider identity changed: %s", job.RunID)
			}
		})
	}
}
func TestNativeEventKeepsLiteralKey(t *testing.T) {
	event := json.RawMessage(`{"source":"aws.s3","detail-type":"Object Created","detail":{"bucket":{"name":"media"},"object":{"key":"output/song+%20 name/vocals.wav","etag":"audio-etag"}}}`)
	h := Handler{newStore(), testSettings}
	job, err := h.Prepare(context.Background(), event)
	if err != nil || job.JobID != "song+%20 name" {
		t.Fatalf("native key changed: %+v %v", job, err)
	}
}
func TestRejectInvalidInputs(t *testing.T) {
	for _, event := range []string{
		`{"bucket":"other","key":"output/song/vocals.wav"}`, `{"bucket":"media","key":"input/song/vocals.wav"}`,
		`{"bucket":"media","key":"output/song/manifest.json"}`, `{"bucket":"media","key":"output/a/b/vocals.wav"}`,
		`{"bucket":"media","key":"output//vocals.wav"}`, `{"bucket":"media","key":"output/../vocals.wav"}`,
		`{"bucket":"media","key":"output/song/vocals.wav","attempt":"../../bad"}`, `{"bucket":"media","key":"output/song/vocals.wav","etag":"old"}`,
		`{"source":"aws.s3","detail-type":"Object Deleted","detail":{"bucket":{"name":"media"},"object":{"key":"output/song/vocals.wav"}}}`,
		`{"bucket":"media","key":"output/song/vocals.wav","attempt":true}`, `{`,
	} {
		t.Run(event, func(t *testing.T) {
			h := Handler{newStore(), testSettings}
			if _, err := h.Prepare(context.Background(), []byte(event)); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}
func TestSourceReplacementRejected(t *testing.T) {
	h, store, job := prepareTest(t)
	store.etag = "changed"
	if _, err := h.Prepare(context.Background(), testEvent); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("source replacement accepted: %v", err)
	}
	if _, err := h.Finalize(context.Background(), job); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("changed input published: %v", err)
	}
}
func TestNewAttemptAfterFailure(t *testing.T) {
	h, _, first := prepareTest(t)
	second, err := h.Prepare(context.Background(), []byte(`{"bucket":"media","key":"output/song/vocals.wav","attempt":"retry-1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.Source, second.Source) || first.JobName == second.JobName || first.RawKey == second.RawKey {
		t.Fatal("retry did not isolate raw/provider job")
	}
}
func TestNoSuccessBeforeValidNormalization(t *testing.T) {
	h, store, job := prepareTest(t)
	raw := rawFixture(t, job)
	raw.Results.Items[0].Start = nil
	saveRaw(t, store, job, raw)
	if _, err := h.Finalize(context.Background(), job); err == nil {
		t.Fatal("missing time accepted")
	}
	if _, ok := store.objects[job.TranscriptKey]; ok {
		t.Fatal("failure published a result")
	}
	saveRaw(t, store, job, rawFixture(t, job))
	if _, err := h.Finalize(context.Background(), job); err != nil {
		t.Fatal(err)
	}
}
func TestMismatchedRawAndPipelineAreRejected(t *testing.T) {
	h, store, job := prepareTest(t)
	raw := rawFixture(t, job)
	raw.JobName = "another-job"
	saveRaw(t, store, job, raw)
	if _, err := h.Finalize(context.Background(), job); err == nil {
		t.Fatal("wrong raw job accepted")
	}
	raw.JobName, raw.Status = job.JobName, "FAILED"
	saveRaw(t, store, job, raw)
	if _, err := h.Finalize(context.Background(), job); err == nil {
		t.Fatal("failed raw job accepted")
	}
	saveRaw(t, store, job, rawFixture(t, job))
	if _, err := h.Finalize(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	h.Settings.PipelineVersion = "2"
	if _, err := h.Prepare(context.Background(), testEvent); err == nil {
		t.Fatal("old pipeline result reused")
	}
}
func TestHandlerDispatchAndSettings(t *testing.T) {
	h, store, job := prepareTest(t)
	prepared, err := h.Handle(context.Background(), testEvent)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.(Job).JobName != job.JobName {
		t.Fatal("prepare dispatch failed")
	}
	saveRaw(t, store, job, rawFixture(t, job))
	h.Settings.Action = "finalize"
	data, _ := json.Marshal(job)
	result, err := h.Handle(context.Background(), data)
	if err != nil || result.(Result).WordCount != 2 {
		t.Fatalf("finalize dispatch failed: %v", err)
	}
	h.Settings.Action = "invalid"
	if _, err := h.Handle(context.Background(), data); err == nil {
		t.Fatal("invalid action accepted")
	}
	h.Settings.Action = "prepare"
	store.failure = &TransientError{Err: errors.New("temporary failure")}
	_, err = h.Handle(context.Background(), testEvent)
	var transient *TransientError
	if !errors.As(err, &transient) {
		t.Fatal("transient error lost before Lambda serialization")
	}
	for _, field := range []string{"MEDIA_BUCKET", "OUTPUT_PREFIX", "ACCOUNT_ID", "PIPELINE_VERSION", "LANGUAGE", "TRANSCRIPTION_ACTION"} {
		t.Setenv(field, "")
	}
	if _, err := SettingsFromEnv(); err == nil {
		t.Fatal("missing environment silently defaulted")
	}
}
