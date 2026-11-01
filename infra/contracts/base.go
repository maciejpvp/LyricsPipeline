// Package contracts defines the versioned boundary between independently deployed projects.
package contracts

import (
	"fmt"
	"strings"

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
	if err := validateReference(ctx, ref, name, region, account); err != nil {
		return Base{}, err
	}
	valid := pulumix.Apply3Err(ref.GetFloat64Output(pulumi.String("contractVersion")), ref.GetStringOutput(pulumi.String("region")), ref.GetStringOutput(pulumi.String("accountId")), func(version float64, r, a string) (bool, error) {
		for _, field := range []struct {
			key   string
			value any
		}{{"contractVersion", version}, {"region", r}, {"accountId", a}} {
			if err := validateOutput(name, field.key, field.value, region, account); err != nil {
				return false, err
			}
		}
		return true, nil
	})
	get := func(key string) pulumi.StringOutput {
		return pulumix.Cast[pulumi.StringOutput](pulumix.Apply2Err(valid, ref.GetStringOutput(pulumi.String(key)), func(_ bool, value string) (string, error) {
			if err := validateOutput(name, key, value, region, account); err != nil {
				return "", err
			}
			return value, nil
		}))
	}
	return Base{BucketName: get("mediaBucket"), BucketARN: get("mediaBucketArn"), QueueURL: get("queueUrl"), QueueARN: get("queueArn"), QueueName: get("queueName"), Objects: ObjectInputs{Input: get("inputPrefix"), Output: get("outputPrefix")}, Ready: []pulumi.Resource{ref}}, nil
}

func validateReference(ctx *pulumi.Context, ref *pulumi.StackReference, name, region, account string) error {
	// Await the referenced stack before callers register resources with inputs that
	// are independent of it (for example, the separation VPC and NAT gateway).
	for _, key := range []string{"contractVersion", "region", "accountId", "mediaBucket", "mediaBucketArn", "queueUrl", "queueArn", "queueName", "inputPrefix", "outputPrefix"} {
		details, err := ref.GetOutputDetails(key)
		if err != nil {
			return fmt.Errorf("base stack %q output %q: could not read output", name, key)
		}
		value := details.Value
		if details.SecretValue != nil {
			value = details.SecretValue
		}
		// GetOutputDetails represents unresolved preview values as nil. Keep the
		// original outputs and deferred checks in Reference rather than inventing defaults.
		if value == nil && ctx.DryRun() {
			continue
		}
		if err := validateOutput(name, key, value, region, account); err != nil {
			return err
		}
	}
	return nil
}

// Do not include output values in errors: contract metadata may be secret.
func validateOutput(stack, key string, value any, region, account string) error {
	problem := ""
	if value == nil {
		problem = "required value is missing, null or unknown"
	} else if key == "contractVersion" {
		version, ok := value.(float64)
		if !ok || version != ContractVersion {
			problem = fmt.Sprintf("expected contract version %d", ContractVersion)
		}
	} else {
		text, ok := value.(string)
		switch {
		case !ok || strings.TrimSpace(text) == "":
			problem = "expected a nonempty string"
		case key == "region" && text != region:
			problem = "does not match the AWS provider region"
		case key == "accountId" && text != account:
			problem = "does not match the AWS provider account"
		}
	}
	if problem != "" {
		return fmt.Errorf("base stack %q output %q: %s", stack, key, problem)
	}
	return nil
}
