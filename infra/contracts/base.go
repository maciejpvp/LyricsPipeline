// Package contracts defines the versioned boundary between independently deployed projects.
package contracts

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumix"
)

const ContractVersion = 1

// ObjectInputs preserves unknown stack outputs during previews.
type ObjectInputs struct{ Input, Output pulumi.StringInput }

type Base struct {
	BucketName, BucketARN         pulumi.StringOutput
	QueueURL, QueueARN, QueueName pulumi.StringOutput
	Objects                       ObjectInputs
	Ready                         []pulumi.Resource
}

// Identity validates the actual provider rather than trusting only local settings.
func Identity(ctx *pulumi.Context, expectedRegion string) (string, error) {
	region, err := aws.GetRegion(ctx, nil)
	if err != nil {
		return "", err
	}
	if region.Region != expectedRegion {
		return "", fmt.Errorf("aws:region %q differs from provider region %q", expectedRegion, region.Region)
	}
	account, err := aws.GetCallerIdentity(ctx, nil)
	if err != nil {
		return "", err
	}
	return account.AccountId, nil
}

func Reference(ctx *pulumi.Context, name, region, account string) (Base, error) {
	if name == "" {
		return Base{}, fmt.Errorf("base stack name must not be empty")
	}
	ref, err := pulumi.NewStackReference(ctx, name, nil)
	if err != nil {
		return Base{}, err
	}
	valid := pulumix.Apply3Err(ref.GetIntOutput(pulumi.String("contractVersion")), ref.GetStringOutput(pulumi.String("region")), ref.GetStringOutput(pulumi.String("accountId")), func(version int, r, a string) (bool, error) {
		if version != ContractVersion || r != region || a != account {
			return false, fmt.Errorf("incompatible base stack: contract=%d region=%q account=%q", version, r, a)
		}
		return true, nil
	})
	get := func(key string) pulumi.StringOutput {
		return pulumix.Cast[pulumi.StringOutput](pulumix.Apply2Err(valid, ref.GetStringOutput(pulumi.String(key)), func(_ bool, value string) (string, error) {
			if value == "" {
				return "", fmt.Errorf("base stack output %s is missing or empty", key)
			}
			return value, nil
		}))
	}
	return Base{BucketName: get("mediaBucket"), BucketARN: get("mediaBucketArn"), QueueURL: get("queueUrl"), QueueARN: get("queueArn"), QueueName: get("queueName"), Objects: ObjectInputs{Input: get("inputPrefix"), Output: get("outputPrefix")}, Ready: []pulumi.Resource{ref}}, nil
}
