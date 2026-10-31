package main

import (
	"fmt"

	"LyricsPipeline/infra/contracts"
	"LyricsPipeline/internal/s3"
	"LyricsPipeline/internal/sqs"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func deploy(ctx *pulumi.Context) error {
	cfg, err := loadConfig(ctx)
	if err != nil {
		return err
	}
	account, err := contracts.Identity(ctx, cfg.Region)
	if err != nil {
		return err
	}
	base, err := createBase(ctx, cfg)
	if err != nil {
		return err
	}
	ctx.Export("contractVersion", pulumi.Int(contracts.ContractVersion))
	ctx.Export("region", pulumi.String(cfg.Region))
	ctx.Export("accountId", pulumi.String(account))
	ctx.Export("mediaBucket", base.BucketName)
	ctx.Export("mediaBucketArn", base.BucketARN)
	ctx.Export("inputPrefix", base.Objects.Input)
	ctx.Export("outputPrefix", base.Objects.Output)
	ctx.Export("queueUrl", base.QueueURL)
	ctx.Export("queueArn", base.QueueARN)
	ctx.Export("queueName", base.QueueName)
	return nil
}

// createBase preserves historical component identities during source refactoring.
func createBase(ctx *pulumi.Context, cfg deploymentConfig) (contracts.Base, error) {
	tags := pulumi.ToStringMap(cfg.Tags)
	media, err := s3.New(ctx, cfg.Name, s3.Args{ComponentType: "custom:storage:Storage", Tags: tags})
	if err != nil {
		return contracts.Base{}, fmt.Errorf("s3: %w", err)
	}
	jobs, err := sqs.New(ctx, cfg.Name, sqs.Args{
		ComponentType: "lyrics:queue:Queue", Settings: cfg.Queue,
		Tags: tags,
	})
	if err != nil {
		return contracts.Base{}, fmt.Errorf("sqs: %w", err)
	}
	// Retain the original queue parent for policy and notification URNs.
	if _, err := configureIngestion(ctx, cfg.Name, ingestionArgs{
		BucketID:    media.BucketID,
		BucketARN:   media.BucketARN,
		QueueARN:    jobs.ARN,
		QueueURL:    jobs.URL,
		InputPrefix: cfg.Objects.Input,
	}, pulumi.Parent(jobs)); err != nil {
		return contracts.Base{}, fmt.Errorf("ingestion: %w", err)
	}
	return contracts.Base{BucketName: media.BucketName, BucketARN: media.BucketARN, QueueURL: jobs.URL, QueueARN: jobs.ARN, QueueName: jobs.Name, Objects: contracts.ObjectInputs{Input: pulumi.String(cfg.Objects.Input), Output: pulumi.String(cfg.Objects.Output)}, Ready: []pulumi.Resource{jobs}}, nil
}
