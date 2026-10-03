package main

import (
	"LyricsPipeline/components"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		config, err := loadInfrastructureConfig(ctx)
		if err != nil {
			return err
		}
		name := config.Name
		tags := config.pulumiTags()

		network, err := components.NewNetwork(ctx, components.NetworkConfig{
			Name:              name,
			AvailabilityZone:  config.AvailabilityZone,
			Tags:              tags,
			VPCCIDR:           "10.42.0.0/16",
			PublicSubnetCIDR:  "10.42.1.0/24",
			PrivateSubnetCIDR: "10.42.2.0/24",
			InternetCIDR:      "0.0.0.0/0",
		})
		if err != nil {
			return err
		}
		storage, err := components.NewStorage(ctx, name, &components.StorageArgs{Tags: tags})
		if err != nil {
			return err
		}
		queue, err := components.NewQueue(ctx, name, storage, config.MaxReceiveCount, config.Visibility, tags)
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
		image := pulumi.StringInput(pulumi.String(config.Image))
		if config.Image == "" {
			image = pulumi.Sprintf("%s:bootstrap", registry.Repository.RepositoryUrl)
		}
		imageDefinition, err := components.NewImage(ctx, name, image, config.Region, config.TaskCPU, config.TaskMemory, storage, queue, roles, tags)
		if err != nil {
			return err
		}
		worker, err := components.NewWorker(ctx, name, config.MinTasks, network, imageDefinition, tags)
		if err != nil {
			return err
		}
		if _, err = components.NewAutoscaling(ctx, name, config.MinTasks, config.MaxTasks, worker, queue.Queue.Name); err != nil {
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
