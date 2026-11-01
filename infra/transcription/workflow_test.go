package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestWorkflowContracts(t *testing.T) {
	asr := runDeployment(t, nil)
	for _, r := range asr.AllResources {
		for _, forbidden := range []string{"aws:ec2/", "aws:ecs/", "aws:ecr/", "aws:s3/"} {
			if strings.HasPrefix(r.TypeToken, forbidden) {
				t.Errorf("transcription creates %s", r.TypeToken)
			}
		}
	}

	for _, action := range []string{"prepare", "finalize"} {
		function := asr.Resources["lyrics-transcription-"+action].Inputs
		if function["runtime"].StringValue() != "provided.al2023" || function["handler"].StringValue() != "bootstrap" || function["architectures"].ArrayValue()[0].StringValue() != "arm64" {
			t.Fatal("Go Lambda must use an ARM64 bootstrap on provided.al2023")
		}
		if function["environment"].ObjectValue()["variables"].ObjectValue()["TRANSCRIPTION_ACTION"].StringValue() != action {
			t.Fatal("shared binary action not configured")
		}
	}
	for _, name := range []string{"lyrics-transcription-start", "lyrics-transcription-failure"} {
		args := asr.Resources[name].Inputs
		if args["deadLetterConfig"].ObjectValue()["arn"].StringValue() == "" || args["retryPolicy"].ObjectValue()["maximumRetryAttempts"].NumberValue() != 185 {
			t.Errorf("missing retry/DLQ: %s", name)
		}
	}
	var pattern struct {
		Detail struct {
			Bucket struct {
				Name []string `json:"name"`
			} `json:"bucket"`
			Object struct {
				Key []struct {
					Wildcard string `json:"wildcard"`
				} `json:"key"`
			} `json:"object"`
		} `json:"detail"`
	}
	if err := json.Unmarshal([]byte(asr.Resources["lyrics-transcription-vocals-created"].Inputs["eventPattern"].StringValue()), &pattern); err != nil {
		t.Fatal(err)
	}
	if pattern.Detail.Bucket.Name[0] != "media" || pattern.Detail.Object.Key[0].Wildcard != "results/stems/*/vocals.wav" {
		t.Fatalf("wrong event filter: %+v", pattern)
	}
	policy := asr.Resources["lyrics-transcription-workflow-policy"].Inputs["policy"].StringValue()
	for _, required := range []string{"transcribe:OutputBucketName", "transcribe:OutputKey", "iam:PassedToService", "transcribe:GetTranscriptionJob"} {
		if !strings.Contains(policy, required) {
			t.Errorf("missing authorization %s", required)
		}
	}
	var permissions struct {
		Statements []struct {
			Actions    json.RawMessage                       `json:"Action"`
			Conditions map[string]map[string]json.RawMessage `json:"Condition"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(policy), &permissions); err != nil {
		t.Fatal(err)
	}
	for _, statement := range permissions.Statements {
		if slices.Contains(policyStrings(t, statement.Actions), "transcribe:StartTranscriptionJob") {
			for _, key := range []string{"transcribe:OutputBucketName", "transcribe:OutputKey"} {
				if !slices.Equal(policyStrings(t, statement.Conditions["Null"][key]), []string{"true"}) {
					t.Fatalf("StartTranscriptionJob must require absent %s", key)
				}
			}
		}
	}
	checkWorkflow(t, asr.Resources["lyrics-transcription-workflow"].Inputs["definition"].StringValue())
}
func checkWorkflow(t *testing.T, raw string) {
	t.Helper()
	var definition struct {
		StartAt        string
		TimeoutSeconds int
		States         map[string]struct {
			Type, Resource, Next, Default, ResultPath, OutputPath, CausePath string
			Seconds                                                          int
			Parameters                                                       map[string]interface{}
			Catch                                                            []struct {
				ErrorEquals []string
				Next        string
			}
			Retry []struct {
				MaxAttempts int
				ErrorEquals []string
			}
		}
	}
	if err := json.Unmarshal([]byte(raw), &definition); err != nil {
		t.Fatal(err)
	}
	if definition.StartAt != "Prepare" || definition.TimeoutSeconds != 21600 || definition.States["Wait"].Seconds != 60 {
		t.Fatal("wrong workflow timing/entrypoint")
	}
	start := definition.States["StartTranscription"]
	if start.Parameters["LanguageCode"] != "en-US" || start.ResultPath != "$.transcribe" {
		t.Fatalf("incorrect start arguments: %+v", start)
	}
	for _, key := range []string{"OutputBucketName", "OutputBucketName.$", "OutputKey", "OutputKey.$"} {
		if _, exists := start.Parameters[key]; exists {
			t.Fatalf("%s would make Transcribe create a write-check object in the media bucket", key)
		}
	}
	if len(start.Catch) != 1 || start.Catch[0].ErrorEquals[0] != "Transcribe.ConflictException" || start.Catch[0].Next != "GetTranscription" {
		t.Fatal("duplicate does not reuse job")
	}
	if definition.States["Prepare"].Retry[0].MaxAttempts != 5 || !slices.Contains(definition.States["Prepare"].Retry[0].ErrorEquals, "TransientError") {
		t.Fatal("Lambda retry count changed")
	}
	if start.Retry[0].MaxAttempts != 5 || definition.States["TranscriptionFailed"].Type != "Fail" {
		t.Fatal("missing bounded retries/failure")
	}
	if definition.States["Prepare"].OutputPath != "$.Payload" || definition.States["Finalize"].Parameters["Payload.$"] != "$" {
		t.Fatal("invalid Lambda payload handling")
	}
	// Optional artifact for the read-only AWS ValidateStateMachineDefinition API.
	if target := os.Getenv("LYRICS_WORKFLOW_VALIDATION_FILE"); target != "" {
		if err := os.WriteFile(target, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
}
