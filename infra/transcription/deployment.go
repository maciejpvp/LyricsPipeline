package main

import (
	"path/filepath"

	"LyricsPipeline/infra/contracts"
	"LyricsPipeline/internal/lambdabuild"

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
	base, err := contracts.Reference(ctx, cfg.BaseStack, cfg.Region, account)
	if err != nil {
		return err
	}
	moduleDir, err := filepath.Abs(filepath.Join(ctx.RootDirectory(), "../.."))
	if err != nil {
		return err
	}
	artifact, err := lambdabuild.Build(ctx.Context(), lambdabuild.Args{
		ModuleDir:    moduleDir,
		Package:      "./transcription/cmd/bootstrap",
		OutputDir:    filepath.Join(moduleDir, "bin/transcription"),
		Architecture: "arm64",
	})
	if err != nil {
		return err
	}
	component, err := createTranscription(ctx, cfg.Name, Args{
		Base: base, Region: cfg.Region, Account: account, Settings: cfg.Transcription,
		Tags: pulumi.ToStringMap(cfg.Tags), Code: pulumi.NewFileArchive(artifact.Path), Architecture: artifact.Architecture,
	})
	if err != nil {
		return err
	}
	ctx.Export("stateMachineArn", component.StateMachineARN)
	ctx.Export("deadLetterQueueUrl", component.DeadLetterURL)
	ctx.Export("mediaBucket", base.BucketName)
	ctx.Export("outputPrefix", base.Objects.Output)
	ctx.Export("region", pulumi.String(cfg.Region))
	ctx.Export("accountId", pulumi.String(account))
	return nil
}
