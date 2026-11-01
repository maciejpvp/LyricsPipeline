package contracts

import (
	"fmt"
	"strings"
	"testing"

	"LyricsPipeline/internal/infratest"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/internals"
)

func baseOutputs() resource.PropertyMap {
	return resource.NewPropertyMapFromMap(map[string]interface{}{
		"contractVersion": 1, "region": "eu-central-1", "accountId": "123456789012", "mediaBucket": "media",
		"mediaBucketArn": "arn:aws:s3:::media", "inputPrefix": "uploads/audio", "outputPrefix": "results/stems",
		"queueUrl": "https://sqs.example/jobs", "queueArn": "arn:aws:sqs:eu-central-1:123456789012:jobs", "queueName": "jobs",
	})
}

func TestReferenceCompatibility(t *testing.T) {
	for _, tc := range []struct {
		key   string
		value interface{}
	}{{"region", "us-east-1"}, {"accountId", "987654321098"}, {"contractVersion", 2}, {"mediaBucket", ""}, {"queueArn", nil}} {
		t.Run(tc.key, func(t *testing.T) {
			m := &infratest.Mocks{ReferenceOutputs: baseOutputs()}
			m.ReferenceOutputs[resource.PropertyKey(tc.key)] = resource.NewPropertyValue(tc.value)
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				b, err := Reference(ctx, "org/LyricsPipeline/dev", "eu-central-1", "123456789012")
				if err == nil {
					ctx.Export("bucket", b.BucketName)
					ctx.Export("queue", b.QueueARN)
				}
				return err
			}, pulumi.WithMocks("consumer", "test", m))
			if err == nil {
				t.Fatal("invalid contract accepted")
			}
		})
	}
}

func TestInvalidOutputsFailBeforeReferenceReturns(t *testing.T) {
	const stack = "org/LyricsPipeline/dev"
	for key := range baseOutputs() {
		for _, invalid := range []string{"missing", "null", "type", "value", "secret"} {
			t.Run(string(key)+"/"+invalid, func(t *testing.T) {
				outputs := baseOutputs()
				switch invalid {
				case "missing":
					delete(outputs, key)
				case "null":
					outputs[key] = resource.NewNullProperty()
				case "type":
					outputs[key] = resource.NewBoolProperty(true)
				case "value":
					if key == "contractVersion" {
						outputs[key] = resource.NewNumberProperty(1.5)
					} else {
						outputs[key] = resource.NewStringProperty(" \t")
					}
				case "secret":
					outputs[key] = resource.MakeSecret(resource.NewStringProperty(""))
				}
				m := &infratest.Mocks{ReferenceOutputs: outputs}
				var directError error
				err := pulumi.RunErr(func(ctx *pulumi.Context) error {
					_, directError = Reference(ctx, stack, "eu-central-1", "123456789012")
					return directError
				}, pulumi.WithMocks("consumer", "test", m))
				if err == nil || directError == nil {
					t.Fatalf("invalid output must fail directly: run=%v reference=%v", err, directError)
				}
				if !strings.Contains(directError.Error(), stack) || !strings.Contains(directError.Error(), string(key)) {
					t.Fatalf("error must identify stack and field: %v", directError)
				}
			})
		}
	}
}

func TestKnownMismatchFailsDirectlyEvenInPreview(t *testing.T) {
	for _, key := range []resource.PropertyKey{"accountId", "region", "contractVersion"} {
		for _, preview := range []bool{false, true} {
			t.Run(string(key)+"/preview="+fmt.Sprint(preview), func(t *testing.T) {
				outputs := baseOutputs()
				const secret = "private-contract-value"
				outputs[key] = resource.MakeSecret(resource.NewStringProperty(secret))
				if key == "contractVersion" {
					outputs[key] = resource.MakeSecret(resource.NewNumberProperty(2))
				}
				// Another unknown field must not prevent validation of known metadata.
				if preview {
					outputs["outputPrefix"] = resource.MakeComputed(resource.NewStringProperty(""))
				}
				m := &infratest.Mocks{ReferenceOutputs: outputs}
				var directError error
				err := pulumi.RunErr(func(ctx *pulumi.Context) error {
					_, directError = Reference(ctx, "org/LyricsPipeline/dev", "eu-central-1", "123456789012")
					return directError
				}, pulumi.WithMocks("consumer", "test", m), func(info *pulumi.RunInfo) { info.DryRun = preview })
				if err == nil || directError == nil || !strings.Contains(directError.Error(), string(key)) {
					t.Fatalf("mismatch did not fail directly: %v %v", err, directError)
				}
				if strings.Contains(err.Error(), secret) {
					t.Fatalf("secret output was included in error: %v", err)
				}
			})
		}
	}
}

func TestValidReferencePreservesOutputs(t *testing.T) {
	for _, preview := range []bool{false, true} {
		t.Run("preview="+fmt.Sprint(preview), func(t *testing.T) {
			outputs := baseOutputs()
			for key, value := range outputs {
				outputs[key] = resource.MakeSecret(value)
			}
			m := &infratest.Mocks{ReferenceOutputs: outputs}
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				base, err := Reference(ctx, "org/LyricsPipeline/dev", "eu-central-1", "123456789012")
				if err != nil {
					return err
				}
				for key, output := range map[string]pulumi.StringOutput{
					"mediaBucket": base.BucketName, "mediaBucketArn": base.BucketARN,
					"queueUrl": base.QueueURL, "queueArn": base.QueueARN, "queueName": base.QueueName,
					"inputPrefix": base.Objects.Input.ToStringOutput(), "outputPrefix": base.Objects.Output.ToStringOutput(),
				} {
					resolved, err := internals.UnsafeAwaitOutput(ctx.Context(), output)
					if err != nil {
						return err
					}
					if !resolved.Known || !resolved.Secret || resolved.Value != baseOutputs()[resource.PropertyKey(key)].StringValue() {
						t.Errorf("%s lost its value, known status or secret flag", key)
					}
					if len(resolved.Dependencies) == 0 {
						t.Errorf("%s lost its stack dependency", key)
					}
					for _, dependency := range resolved.Dependencies {
						if dependency != base.Ready[0] {
							t.Errorf("%s acquired an unrelated dependency", key)
						}
					}
				}
				return nil
			}, pulumi.WithMocks("consumer", "test", m), func(info *pulumi.RunInfo) { info.DryRun = preview })
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestUnknownOutputsRequirePreview(t *testing.T) {
	for key := range baseOutputs() {
		for _, preview := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/preview=%t", key, preview), func(t *testing.T) {
				outputs := baseOutputs()
				outputs[key] = resource.MakeComputed(outputs[key])
				m := &infratest.Mocks{ReferenceOutputs: outputs}
				var directError error
				err := pulumi.RunErr(func(ctx *pulumi.Context) error {
					base, err := Reference(ctx, "org/LyricsPipeline/dev", "eu-central-1", "123456789012")
					directError = err
					if err == nil {
						ctx.Export("bucket", base.BucketName)
						ctx.Export("bucketArn", base.BucketARN)
						ctx.Export("queue", base.QueueURL)
						ctx.Export("queueArn", base.QueueARN)
						ctx.Export("queueName", base.QueueName)
						ctx.Export("input", base.Objects.Input)
						ctx.Export("output", base.Objects.Output)
					}
					return err
				}, pulumi.WithMocks("consumer", "test", m), func(info *pulumi.RunInfo) { info.DryRun = preview })
				if preview && err != nil {
					t.Fatalf("unknown preview output failed: %v", err)
				}
				if !preview && (err == nil || directError == nil) {
					t.Fatalf("unknown update output must fail directly: run=%v reference=%v", err, directError)
				}
			})
		}
	}
}
func TestIdentityMismatchBeforeResources(t *testing.T) {
	m := &infratest.Mocks{ProviderRegion: "us-east-1"}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error { _, err := Identity(ctx, "eu-central-1"); return err }, pulumi.WithMocks("consumer", "test", m))
	if err == nil || len(m.AllResources) != 0 {
		t.Fatalf("mismatch accepted: %v", err)
	}
}
