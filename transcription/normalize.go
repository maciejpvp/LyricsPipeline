package transcription

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func number(raw json.RawMessage, field string) (float64, error) {
	value := bytes.TrimSpace(raw)
	var result float64
	var err error
	if len(value) == 0 || bytes.Equal(value, []byte("null")) {
		return 0, fmt.Errorf("missing or invalid %s", field)
	}
	if value[0] == '"' {
		var text string
		if err = json.Unmarshal(value, &text); err == nil {
			result, err = strconv.ParseFloat(text, 64)
		}
	} else {
		err = json.Unmarshal(value, &result)
	}
	if err != nil || math.IsInf(result, 0) || math.IsNaN(result) {
		return 0, fmt.Errorf("invalid or non-finite %s", field)
	}
	return result, nil
}
func Normalize(raw RawTranscript, job Job, language string) (Transcript, error) {
	if job.Source == nil || len(raw.Results.Transcripts) == 0 || raw.Results.Transcripts[0].Transcript == nil || raw.Results.Items == nil {
		return Transcript{}, errors.New("missing transcript text or items")
	}
	words := make([]Word, 0, len(raw.Results.Items))
	leading := ""
	previousStart := 0.0
	for _, item := range raw.Results.Items {
		if len(item.Alternatives) == 0 || item.Alternatives[0].Content == "" {
			return Transcript{}, errors.New("empty transcript item")
		}
		alternative := item.Alternatives[0]
		if item.Type == "punctuation" {
			if len(words) > 0 {
				words[len(words)-1].Text += alternative.Content
			} else {
				leading += alternative.Content
			}
			continue
		}
		if item.Type != "pronunciation" {
			return Transcript{}, errors.New("unsupported transcript item")
		}
		start, err := number(item.Start, "start")
		if err != nil {
			return Transcript{}, err
		}
		end, err := number(item.End, "end")
		if err != nil {
			return Transcript{}, err
		}
		if start < 0 || end < start || start < previousStart {
			return Transcript{}, errors.New("invalid or unordered word interval")
		}
		var confidence *float64
		if value := bytes.TrimSpace(alternative.Confidence); len(value) > 0 && !bytes.Equal(value, []byte("null")) {
			parsed, err := number(value, "confidence")
			if err != nil {
				return Transcript{}, err
			}
			if parsed < 0 || parsed > 1 {
				return Transcript{}, errors.New("confidence outside [0,1]")
			}
			confidence = &parsed
		}
		words = append(words, Word{Text: leading + alternative.Content, Start: start, End: end, Confidence: confidence})
		leading = ""
		previousStart = start
	}
	text := *raw.Results.Transcripts[0].Transcript
	if (strings.TrimSpace(text) != "" && len(words) == 0) || leading != "" {
		return Transcript{}, errors.New("text has no timed words")
	}
	return Transcript{SchemaVersion: 1, PipelineVersion: job.PipelineVersion, JobID: job.JobID, Language: language, Source: *job.Source, Text: text, Words: words}, nil
}
