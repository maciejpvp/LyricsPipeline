package contracts

import (
	"testing"

	"LyricsPipeline/internal/infratest"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
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
func TestIdentityMismatchBeforeResources(t *testing.T) {
	m := &infratest.Mocks{ProviderRegion: "us-east-1"}
	err := pulumi.RunErr(func(ctx *pulumi.Context) error { _, err := Identity(ctx, "eu-central-1"); return err }, pulumi.WithMocks("consumer", "test", m))
	if err == nil || len(m.AllResources) != 0 {
		t.Fatalf("mismatch accepted: %v", err)
	}
}
