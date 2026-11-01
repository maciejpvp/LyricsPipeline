package main

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Only the finalization Lambda archives provider output. Transcribe and the
// workflow cannot create result or write-check objects in the media bucket.
func TestTranscribeS3Permissions(t *testing.T) {
	mocks := runDeployment(t, nil)
	for _, name := range []string{"lyrics-transcription-transcribe-policy", "lyrics-transcription-workflow-policy"} {
		t.Run(name, func(t *testing.T) {
			var policy struct {
				Statements []struct {
					Effect     string
					Actions    json.RawMessage `json:"Action"`
					Resources  json.RawMessage `json:"Resource"`
					Conditions map[string]any  `json:"Condition"`
				} `json:"Statement"`
			}
			if err := json.Unmarshal([]byte(mocks.Resources[name].Inputs["policy"].StringValue()), &policy); err != nil {
				t.Fatal(err)
			}
			permissions := map[string][]string{}
			for _, statement := range policy.Statements {
				for _, action := range policyStrings(t, statement.Actions) {
					if !strings.HasPrefix(action, "s3:") {
						continue
					}
					if statement.Effect != "Allow" {
						t.Fatal("unexpected S3 deny")
					}
					if len(statement.Conditions) != 0 {
						t.Fatal("bucket probe/list must not require object-prefix context")
					}
					permissions[action] = append(permissions[action], policyStrings(t, statement.Resources)...)
				}
			}
			expected := map[string][]string{
				"s3:ListBucket": {"arn:aws:s3:::media"},
				"s3:GetObject":  {"arn:aws:s3:::media/results/stems/*"},
			}
			for _, resources := range permissions {
				slices.Sort(resources)
			}
			if !reflect.DeepEqual(permissions, expected) {
				t.Fatalf("unexpected S3 access:\ngot %v\nwant %v", permissions, expected)
			}
		})
	}
}

func TestOnlyFinalizerCanReadManagedTranscript(t *testing.T) {
	mocks := runDeployment(t, nil)
	for _, action := range []string{"prepare", "finalize"} {
		var policy struct {
			Statements []struct {
				Actions   json.RawMessage `json:"Action"`
				Resources json.RawMessage `json:"Resource"`
			} `json:"Statement"`
		}
		name := "lyrics-transcription-" + action + "-policy"
		if err := json.Unmarshal([]byte(mocks.Resources[name].Inputs["policy"].StringValue()), &policy); err != nil {
			t.Fatal(err)
		}
		var jobs []string
		for _, statement := range policy.Statements {
			if slices.Contains(policyStrings(t, statement.Actions), "transcribe:GetTranscriptionJob") {
				jobs = append(jobs, policyStrings(t, statement.Resources)...)
			}
		}
		if action == "prepare" && len(jobs) != 0 {
			t.Fatal("prepare must not read provider transcripts")
		}
		if action == "finalize" && !reflect.DeepEqual(jobs, []string{"arn:aws:transcribe:eu-central-1:123456789012:transcription-job/lyrics-*"}) {
			t.Fatalf("unexpected finalizer Transcribe access: %v", jobs)
		}
	}
}

func TestLambdaSourceFolderReadPermissions(t *testing.T) {
	mocks := runDeployment(t, nil)
	for _, name := range []string{"lyrics-transcription-prepare-policy", "lyrics-transcription-finalize-policy"} {
		t.Run(name, func(t *testing.T) {
			var policy struct {
				Statements []struct {
					Actions   json.RawMessage `json:"Action"`
					Resources json.RawMessage `json:"Resource"`
				} `json:"Statement"`
			}
			if err := json.Unmarshal([]byte(mocks.Resources[name].Inputs["policy"].StringValue()), &policy); err != nil {
				t.Fatal(err)
			}
			var reads []string
			for _, statement := range policy.Statements {
				if slices.Contains(policyStrings(t, statement.Actions), "s3:GetObject") {
					reads = append(reads, policyStrings(t, statement.Resources)...)
				}
			}
			slices.Sort(reads)
			expected := []string{
				"arn:aws:s3:::media/results/stems/*",
				"arn:aws:s3:::media/results/stems/*/transcription/*",
			}
			if !reflect.DeepEqual(reads, expected) {
				t.Fatalf("unexpected read scope: got %v, want %v", reads, expected)
			}
		})
	}
}

func policyStrings(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return values
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return []string{value}
}
