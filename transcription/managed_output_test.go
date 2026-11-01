package transcription

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
)

type transcriptReaderFunc func(context.Context, string) ([]byte, error)

func (f transcriptReaderFunc) Read(ctx context.Context, name string) ([]byte, error) {
	return f(ctx, name)
}

func TestFinalizeArchivesManagedOutputWithoutProbe(t *testing.T) {
	h, store, job := prepareTest(t)
	raw := rawFixture(t, job)
	data, _ := json.Marshal(raw)
	// Preserve provider metadata even when normalization does not use it.
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	document["extra_provider_metadata"] = map[string]any{"keep": true}
	data, _ = json.Marshal(document)
	reads := 0
	h.Provider = transcriptReaderFunc(func(_ context.Context, name string) ([]byte, error) {
		reads++
		if name != job.JobName {
			t.Fatal("wrong provider job")
		}
		return data, nil
	})
	for range 2 {
		result, err := h.Finalize(t.Context(), job)
		if err != nil || result.WordCount != 2 {
			t.Fatalf("finalize: %+v %v", result, err)
		}
	}
	if reads != 1 || string(store.objects[job.RawKey]) != string(data) || store.writes[job.RawKey] != 1 || store.writes[job.TranscriptKey] != 1 {
		t.Fatalf("raw output was lost or duplicated: reads=%d writes=%v", reads, store.writes)
	}
	for key := range store.objects {
		if key != job.RawKey && key != job.TranscriptKey && key != "output/song/transcription/source.json" {
			t.Fatalf("unexpected object created: %s", key)
		}
	}
}

func TestManagedOutputFailuresDoNotPublish(t *testing.T) {
	for _, failure := range []string{"download", "invalid JSON", "wrong job", "failed job", "invalid timing"} {
		t.Run(failure, func(t *testing.T) {
			h, store, job := prepareTest(t)
			raw := rawFixture(t, job)
			switch failure {
			case "wrong job":
				raw.JobName = "another-job"
			case "failed job":
				raw.Status = "FAILED"
			case "invalid timing":
				raw.Results.Items[0].Start = nil
			}
			h.Provider = transcriptReaderFunc(func(context.Context, string) ([]byte, error) {
				if failure == "download" {
					return nil, &TransientError{Err: errors.New("temporary download failure")}
				}
				if failure == "invalid JSON" {
					return []byte("{"), nil
				}
				return json.Marshal(raw)
			})
			_, err := h.Finalize(t.Context(), job)
			if err == nil {
				t.Fatal("invalid managed output accepted")
			}
			var transient *TransientError
			if errors.As(err, &transient) != (failure == "download") {
				t.Fatalf("retry classification lost: %v", err)
			}
			if _, exists := store.objects[job.TranscriptKey]; exists {
				t.Fatal("failure published success")
			}
			if _, exists := store.objects[job.RawKey]; exists {
				t.Fatal("invalid provider result archived")
			}
		})
	}
}

func TestExistingRawOutputDoesNotNeedManagedProvider(t *testing.T) {
	h, store, job := prepareTest(t)
	saveRaw(t, store, job, rawFixture(t, job))
	h.Provider = transcriptReaderFunc(func(context.Context, string) ([]byte, error) {
		t.Fatal("existing raw output must remain usable without provider access")
		return nil, nil
	})
	result, err := h.Finalize(t.Context(), job)
	if err != nil || !reflect.DeepEqual(result, Result{Bucket: "media", TranscriptKey: job.TranscriptKey, WordCount: 2}) {
		t.Fatalf("existing output compatibility: %+v %v", result, err)
	}
}

func TestConcurrentManagedOutputPublication(t *testing.T) {
	h, store, job := prepareTest(t)
	data, _ := json.Marshal(rawFixture(t, job))
	h.Provider = transcriptReaderFunc(func(context.Context, string) ([]byte, error) { return data, nil })
	var group sync.WaitGroup
	failures := make(chan error, 8)
	for range 8 {
		group.Go(func() { _, err := h.Finalize(t.Context(), job); failures <- err })
	}
	group.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if store.writes[job.RawKey] != 1 || store.writes[job.TranscriptKey] != 1 {
		t.Fatalf("concurrent finalizers duplicated publication: %v", store.writes)
	}
}
