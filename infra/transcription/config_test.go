package main

import (
	"encoding/json"
	"testing"

	"LyricsPipeline/internal/configtest"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func readTestConfig(t *testing.T, values map[string]string) error {
	t.Helper()
	return pulumi.RunErr(func(ctx *pulumi.Context) error { _, err := loadConfig(ctx); return err }, pulumi.WithMocks("LyricsPipeline-Transcription", "test", &infraMocks{}), func(info *pulumi.RunInfo) { info.Config = values })
}
func TestTranscriptionConfiguration(t *testing.T) {
	for _, patch := range []string{`null`, `{}`, `{"unknown":1}`, `{"language":"pl-PL"}`, `{"pipelineVersion":""}`, `{"pollSeconds":0}`, `{"timeoutSeconds":1}`, `{"timeoutSeconds":31536001}`, `{"retryAttempts":0}`, `{"retryAttempts":6}`, `{"retryIntervalSeconds":0}`, `{"lambdaTimeoutSeconds":901}`, `{"lambdaMemoryMiB":127}`, `{"logRetentionDays":0}`, `{"deadLetterRetentionDays":15}`, `{"eventMaxAgeSeconds":59}`, `{"eventRetryAttempts":186}`, `{"pollSeconds":null}`} {
		t.Run(patch, func(t *testing.T) {
			values := configtest.ValuesAt(t, "Pulumi.yaml")
			values["LyricsPipeline-Transcription:baseStack"] = "org/LyricsPipeline/dev"
			if patch == `{}` {
				values["LyricsPipeline-Transcription:transcription"] = patch
			} else {
				configtest.EditObject(t, values, "LyricsPipeline-Transcription:transcription", patch)
			}
			if err := readTestConfig(t, values); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
	values := configtest.StackValuesAt(t, "Pulumi.yaml", "dev")
	if err := readTestConfig(t, values); err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(values["LyricsPipeline-Transcription:transcription"]), &fields); err != nil {
		t.Fatal(err)
	}
	for key := range fields {
		t.Run("missing_"+key, func(t *testing.T) {
			copy := configtest.ValuesAt(t, "Pulumi.yaml")
			copy["LyricsPipeline-Transcription:baseStack"] = "org/LyricsPipeline/dev"
			var object map[string]json.RawMessage
			if err := json.Unmarshal([]byte(copy["LyricsPipeline-Transcription:transcription"]), &object); err != nil {
				t.Fatal(err)
			}
			delete(object, key)
			raw, err := json.Marshal(object)
			if err != nil {
				t.Fatal(err)
			}
			copy["LyricsPipeline-Transcription:transcription"] = string(raw)
			if err := readTestConfig(t, copy); err == nil {
				t.Fatal("missing field accepted")
			}
		})
	}
}
