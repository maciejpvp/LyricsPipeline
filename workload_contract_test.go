package main

import (
	"encoding/json"
	"reflect"
	"testing"

	"LyricsPipeline/internal/config"
	"LyricsPipeline/internal/configtest"
	"LyricsPipeline/internal/ecs"
	"LyricsPipeline/internal/iam"
	"LyricsPipeline/internal/ingestion"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
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
	raw := mocks.resources[name].Inputs["containerDefinitions"].StringValue()
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
	var cfg config.Deployment
	loadErr := pulumi.RunErr(func(ctx *pulumi.Context) error { var err error; cfg, err = config.Load(ctx); return err }, pulumi.WithMocks("LyricsPipeline", "test", &infraMocks{}), func(info *pulumi.RunInfo) { info.Config = configtest.ProjectValues(t) })
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	cfg.Objects = config.ObjectLayout{Input: "uploads/audio", Output: "results/stems"}
	mocks := &infraMocks{}
	const bucketARN = "arn:aws:s3:::media"
	const queueARN = "arn:aws:sqs:eu-central-1:123456789012:jobs"
	err := pulumi.RunErr(func(ctx *pulumi.Context) error {
		roles, err := iam.New(ctx, "custom", iam.Args{BucketARN: pulumi.String(bucketARN), QueueARN: pulumi.String(queueARN), Objects: cfg.Objects})
		if err != nil {
			return err
		}
		if _, err := ingestion.New(ctx, "custom", ingestion.Args{
			BucketID: pulumi.ID("media"), BucketARN: pulumi.String(bucketARN), QueueARN: pulumi.String(queueARN), QueueURL: pulumi.String("https://sqs.example/jobs"), InputPrefix: cfg.Objects.Input,
		}); err != nil {
			return err
		}
		_, err = ecs.NewTaskDefinition(ctx, "custom", ecs.TaskDefinitionArgs{
			Image: pulumi.String("registry/image:tag"), Region: cfg.Region, Settings: cfg.Task, Objects: cfg.Objects,
			BucketName: pulumi.String("media"), QueueURL: pulumi.String("https://sqs.example/jobs"), ExecutionRoleARN: roles.ExecutionRoleARN, TaskRoleARN: roles.TaskRoleARN,
		})
		return err
	}, pulumi.WithMocks("LyricsPipeline", "test", mocks))
	if err != nil {
		t.Fatal(err)
	}
	container := readContainer(t, mocks, "custom-task")
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
	if logs.Driver != "awslogs" || logs.Options.Group != "custom-logs" || logs.Options.Region != "eu-central-1" || logs.Options.StreamPrefix != "worker" {
		t.Fatalf("invalid logging: %+v", logs)
	}
	notification := mocks.resources["custom-notification"].Inputs["queues"].ArrayValue()
	if notification[0].ObjectValue()["filterPrefix"].StringValue() != "uploads/audio/" {
		t.Fatal("notification does not use the input prefix")
	}

	assertPolicy(t, mocks, "custom-task-policy", "policy", `{"Version":"2012-10-17","Statement":[
  {"Effect":"Allow","Action":["s3:ListBucket"],"Resource":"arn:aws:s3:::media"},
  {"Effect":"Allow","Action":["s3:GetObject"],"Resource":"arn:aws:s3:::media/uploads/audio/*"},
  {"Effect":"Allow","Action":["s3:GetObject","s3:PutObject"],"Resource":"arn:aws:s3:::media/results/stems/*"},
  {"Effect":"Allow","Action":["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:ChangeMessageVisibility","sqs:GetQueueAttributes"],"Resource":"arn:aws:sqs:eu-central-1:123456789012:jobs"}
 ]}`)
	assertPolicy(t, mocks, "custom-queue-policy", "policy", `{"Version":"2012-10-17","Statement":[{
  "Effect":"Allow","Principal":{"Service":"s3.amazonaws.com"},"Action":"sqs:SendMessage","Resource":"arn:aws:sqs:eu-central-1:123456789012:jobs","Condition":{"ArnEquals":{"aws:SourceArn":"arn:aws:s3:::media"}}
 }]}`)
	for _, role := range []string{"custom-execution-role", "custom-task-role"} {
		assertPolicy(t, mocks, role, "assumeRolePolicy", `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"sts:AssumeRole","Principal":{"Service":"ecs-tasks.amazonaws.com"}}]}`)
	}
}

func assertPolicy(t *testing.T, mocks *infraMocks, name string, field resource.PropertyKey, expected string) {
	t.Helper()
	var got, want any
	if err := json.Unmarshal([]byte(mocks.resources[name].Inputs[field].StringValue()), &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalizePolicy(got), normalizePolicy(want)) {
		t.Errorf("%s %s: got %#v, want %#v", name, field, got, want)
	}
}
