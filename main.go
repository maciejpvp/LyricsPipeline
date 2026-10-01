package main

import (
	"LyricsPipeline/components"
	"os"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		get := func(name, fallback string) string {
			if value := os.Getenv(name); value != "" {
				return value
			}
			return fallback
		}
		image := get("VOCAL_EXTRACTOR_IMAGE", "public.ecr.aws/docker/library/python:3.11-slim")
		if image == "" {
			image = "public.ecr.aws/docker/library/python:3.11-slim"
		}
		region := get("AWS_REGION", "eu-central-1")
		az := get("AWS_AVAILABILITY_ZONE", region+"a")
		taskCPU, taskMemory := get("TASK_CPU", "2048"), get("TASK_MEMORY", "8192")
		maxTasks, visibility, maxReceive := 10, 1800, 5
		name := "vocal-extractor"
		tags := pulumi.StringMap{"Purpose": pulumi.String(name)}

		network, err := components.NewNetwork(ctx, name, az, tags)
		if err != nil {
			return err
		}
		storage, err := components.NewStorage(ctx, name, &components.StorageArgs{Tags: tags})
		if err != nil {
			return err
		}
		queue, err := components.NewQueue(ctx, name, storage, maxReceive, visibility, tags)
		if err != nil {
			return err
		}
		roles, err := components.NewWorkerIAM(ctx, name, storage, queue, tags)
		if err != nil {
			return err
		}
		registry, err := components.NewRegistry(ctx, name, tags)
		if err != nil {
			return err
		}
		imageDefinition, err := components.NewImage(ctx, name, image, region, taskCPU, taskMemory, storage, queue, roles, tags)
		if err != nil {
			return err
		}
		worker, err := components.NewWorker(ctx, name, network, imageDefinition, tags)
		if err != nil {
			return err
		}
		if err = components.NewAutoscaling(ctx, name, maxTasks, worker, queue.Queue.Name); err != nil {
			return err
		}

		ctx.Export("mediaBucket", storage.BucketName)
		ctx.Export("queueUrl", queue.Queue.Url)
		ctx.Export("queueArn", queue.Queue.Arn)
		ctx.Export("repositoryUrl", registry.Repository.RepositoryUrl)
		ctx.Export("clusterName", worker.Cluster.Name)
		ctx.Export("serviceName", worker.Service.Name)

		return nil
	})
}
