package config

import (
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pulumiconfig "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

// Load reads the effective configuration already resolved by Pulumi. Shared
// values belong to Pulumi.yaml; there are no application defaults in Go.
func Load(ctx *pulumi.Context) (Deployment, error) {
	cfg := pulumiconfig.New(ctx, "")
	for _, key := range []string{"deployment", "deploymentImage"} {
		if _, err := cfg.Try(key); err == nil {
			return Deployment{}, fmt.Errorf("legacy configuration %q: migrate to native settings; see README.md#stack-configuration", key)
		}
	}
	var d Deployment
	var err error
	if d.Name, err = cfg.Try("name"); err != nil {
		return Deployment{}, err
	}
	if d.Region, err = pulumiconfig.New(ctx, "aws").Try("region"); err != nil {
		return Deployment{}, err
	}
	if d.Image, err = cfg.Try("image"); err != nil {
		return Deployment{}, err
	}
	if err = cfg.TryObject("tags", &d.Tags); err != nil {
		return Deployment{}, fmt.Errorf("tags: %w", err)
	}
	if d.Tags == nil {
		return Deployment{}, fmt.Errorf("tags: expected a configuration object, got null")
	}

	var vpc vpcSettings
	if err = cfg.TryObject("vpc", &vpc); err != nil {
		return Deployment{}, fmt.Errorf("vpc: %w", err)
	}
	if err = vpc.apply(&d); err != nil {
		return Deployment{}, err
	}
	var s3 s3Settings
	if err = cfg.TryObject("s3", &s3); err != nil {
		return Deployment{}, fmt.Errorf("s3: %w", err)
	}
	d.Objects = ObjectLayout{Input: s3.InputPrefix, Output: s3.OutputPrefix}
	var ecs ecsSettings
	if err = cfg.TryObject("ecs", &ecs); err != nil {
		return Deployment{}, fmt.Errorf("ecs: %w", err)
	}
	if err = ecs.apply(&d.Task); err != nil {
		return Deployment{}, err
	}
	var sqs sqsSettings
	if err = cfg.TryObject("sqs", &sqs); err != nil {
		return Deployment{}, fmt.Errorf("sqs: %w", err)
	}
	if err = sqs.apply(&d.Queue); err != nil {
		return Deployment{}, err
	}
	var scaling autoscalingSettings
	if err = cfg.TryObject("autoscaling", &scaling); err != nil {
		return Deployment{}, fmt.Errorf("autoscaling: %w", err)
	}
	if err = scaling.apply(&d.Scaling); err != nil {
		return Deployment{}, err
	}
	if err = d.Validate(); err != nil {
		return Deployment{}, err
	}
	return d, nil
}
