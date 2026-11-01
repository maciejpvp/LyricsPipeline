package transcription

import (
	"encoding/json"
	"testing"
)

func TestInvalidTimesNeverInvented(t *testing.T) {
	for _, tc := range []struct{ field, value string }{{"start", "null"}, {"end", `"NaN"`}, {"start", `"-1"`}, {"end", `"0.5"`}, {"start", "true"}, {"start", ""}, {"end", `"+Inf"`}} {
		t.Run(tc.field+tc.value, func(t *testing.T) {
			_, _, job := prepareTest(t)
			raw := rawFixture(t, job)
			if tc.field == "start" {
				raw.Results.Items[0].Start = json.RawMessage(tc.value)
			} else {
				raw.Results.Items[0].End = json.RawMessage(tc.value)
			}
			if _, err := Normalize(raw, job, "en-US"); err == nil {
				t.Fatal("invalid interval accepted")
			}
		})
	}
}
func TestEmptyTranscriptAndMissingConfidence(t *testing.T) {
	_, _, job := prepareTest(t)
	raw := rawFixture(t, job)
	raw.Results.Items[0].Alternatives[0].Confidence = nil
	document, err := Normalize(raw, job, "en-US")
	if err != nil || document.Words[0].Confidence != nil {
		t.Fatalf("missing confidence not null: %v", err)
	}
	empty := ""
	raw.Results.Transcripts[0].Transcript = &empty
	raw.Results.Items = []RawItem{}
	document, err = Normalize(raw, job, "en-US")
	if err != nil || document.Words == nil || len(document.Words) != 0 {
		t.Fatalf("empty transcript: %v", err)
	}
	nonempty := "missing timed words"
	raw.Results.Transcripts[0].Transcript = &nonempty
	if _, err := Normalize(raw, job, "en-US"); err == nil {
		t.Fatal("untimed text accepted")
	}
}
func TestMalformedRawItems(t *testing.T) {
	for _, kind := range []string{"nonmonotonic", "confidence", "alternatives", "items", "text", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			_, _, job := prepareTest(t)
			raw := rawFixture(t, job)
			switch kind {
			case "nonmonotonic":
				raw.Results.Items[1].Start = json.RawMessage(`"0.5"`)
			case "confidence":
				raw.Results.Items[0].Alternatives[0].Confidence = json.RawMessage(`"1.01"`)
			case "alternatives":
				raw.Results.Items[0].Alternatives = nil
			case "items":
				raw.Results.Items = nil
			case "text":
				raw.Results.Transcripts = nil
			case "unknown":
				raw.Results.Items[0].Type = "unknown"
			}
			if _, err := Normalize(raw, job, "en-US"); err == nil {
				t.Fatal("invalid raw item accepted")
			}
		})
	}
}
