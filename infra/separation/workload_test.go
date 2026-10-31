package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"LyricsPipeline/internal/infratest"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

// This test-side representation checks the public ECS document independently
// of the private structs used to produce it.
type workerContainer struct {
	Name        string `json:"name"`
	Image       string `json:"image"`
	StopTimeout int    `json:"stopTimeout"`
	Essential   bool   `json:"essential"`
	Environment []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"environment"`
	HealthCheck struct {
		Command     []string `json:"command"`
		Interval    int      `json:"interval"`
		Timeout     int      `json:"timeout"`
		Retries     int      `json:"retries"`
		StartPeriod int      `json:"startPeriod"`
	} `json:"healthCheck"`
	LogConfiguration struct {
		Driver  string `json:"logDriver"`
		Options struct {
			Group        string `json:"awslogs-group"`
			Region       string `json:"awslogs-region"`
			StreamPrefix string `json:"awslogs-stream-prefix"`
		} `json:"options"`
	} `json:"logConfiguration"`
}

func readContainer(t *testing.T, mocks *infraMocks, name string) workerContainer {
	t.Helper()
	raw := mocks.Resources[name].Inputs["containerDefinitions"].StringValue()
	var containers []workerContainer
	if err := json.Unmarshal([]byte(raw), &containers); err != nil {
		t.Fatal(err)
	}
	if len(containers) != 1 {
		t.Fatalf("expected one worker container, got %d", len(containers))
	}
	return containers[0]
}

func TestImageSelection(t *testing.T) {
	for _, tc := range []struct{ name, image, want string }{
		{name: "bootstrap", want: "registry.example/vocal-extractor-image:bootstrap"},
		{name: "configured tag", image: "registry/image:tag", want: "registry/image:tag"},
		{name: "published digest", image: "registry/image@sha256:digest", want: "registry/image@sha256:digest"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := map[string]string{}
			if tc.image != "" {
				values["image"] = tc.image
			}
			container := readContainer(t, runDeployment(t, values), "vocal-extractor-task")
			if container.Image != tc.want {
				t.Fatalf("image=%q, want %q", container.Image, tc.want)
			}
		})
	}
}

func TestSharedObjectLayout(t *testing.T) {
	mocks := runDeployment(t, nil)
	container := readContainer(t, mocks, "vocal-extractor-task")
	environment := map[string]string{}
	for _, entry := range container.Environment {
		environment[entry.Name] = entry.Value
	}
	expected := map[string]string{
		"AWS_REGION": "eu-central-1", "MEDIA_BUCKET": "media", "QUEUE_URL": "https://sqs.example/jobs",
		"INPUT_PREFIX": "uploads/audio", "OUTPUT_PREFIX": "results/stems",
	}
	if !reflect.DeepEqual(environment, expected) {
		t.Fatalf("environment: got %v, want %v", environment, expected)
	}
	if container.Name != "worker" || !container.Essential || container.StopTimeout != 120 {
		t.Fatalf("invalid container lifecycle: %+v", container)
	}
	health := container.HealthCheck
	if !reflect.DeepEqual(health.Command, []string{"CMD-SHELL", "python healthcheck.py"}) || health.Interval != 60 || health.Timeout != 5 || health.Retries != 3 || health.StartPeriod != 120 {
		t.Fatalf("invalid health check: %+v", health)
	}
	logs := container.LogConfiguration
	if logs.Driver != "awslogs" || logs.Options.Group != "vocal-extractor-logs" || logs.Options.Region != "eu-central-1" || logs.Options.StreamPrefix != "worker" {
		t.Fatalf("invalid logging: %+v", logs)
	}
	assertPolicy(t, mocks, "vocal-extractor-task-policy", "policy", `{"Version":"2012-10-17","Statement":[
  {"Effect":"Allow","Action":["s3:ListBucket"],"Resource":"arn:aws:s3:::media"},
  {"Effect":"Allow","Action":["s3:GetObject"],"Resource":"arn:aws:s3:::media/uploads/audio/*"},
  {"Effect":"Allow","Action":["s3:GetObject","s3:PutObject"],"Resource":"arn:aws:s3:::media/results/stems/*"},
  {"Effect":"Allow","Action":["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:ChangeMessageVisibility","sqs:GetQueueAttributes"],"Resource":"arn:aws:sqs:eu-central-1:123456789012:jobs"}
 ]}`)
	for _, role := range []string{"vocal-extractor-execution-role", "vocal-extractor-task-role"} {
		assertPolicy(t, mocks, role, "assumeRolePolicy", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"ecs-tasks.amazonaws.com"}}]}`)
	}
}

func assertPolicy(t *testing.T, mocks *infraMocks, name string, field resource.PropertyKey, expected string) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal([]byte(mocks.Resources[name].Inputs[field].StringValue()), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(infratest.NormalizePolicy(got), infratest.NormalizePolicy(want)) {
		t.Errorf("%s %s: got %#v, want %#v", name, field, got, want)
	}
}
