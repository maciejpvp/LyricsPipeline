package main

import (
	"fmt"

	"LyricsPipeline/infra/contracts"
	"LyricsPipeline/internal/autoscaling"
	"LyricsPipeline/internal/ecr"
	"LyricsPipeline/internal/vpc"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
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
	if err = validateLocation(ctx, cfg); err != nil {
		return err
	}
	base, err := contracts.Reference(ctx, cfg.BaseStack, cfg.Region, account)
	if err != nil {
		return err
	}
	tags := pulumi.ToStringMap(cfg.Tags)
	vpcNetwork, err := vpc.New(ctx, cfg.Name, vpc.Args{
		ComponentType: "lyrics:network:Network", Settings: cfg.Network,
		Region: cfg.Region, AvailabilityZone: cfg.AvailabilityZone,
		InterfaceServices: []string{"ecr.api", "ecr.dkr", "sqs", "logs"}, Tags: tags,
	})
	if err != nil {
		return err
	}
	roles, err := createWorkerIdentity(ctx, cfg.Name, base, tags)
	if err != nil {
		return fmt.Errorf("iam: %w", err)
	}
	images, err := ecr.New(ctx, cfg.Name, ecr.Args{ComponentType: "lyrics:registry:Registry", Tags: tags})
	if err != nil {
		return fmt.Errorf("ecr: %w", err)
	}
	taskDefinition, err := createTask(ctx, cfg, base, image(cfg.Image, images.RepositoryURL), roles)
	if err != nil {
		return fmt.Errorf("ecs task definition: %w", err)
	}
	servicePrerequisites := append([]pulumi.Resource{}, vpcNetwork.ConnectivityReady...)
	servicePrerequisites = append(servicePrerequisites, roles.PermissionsReady...)
	servicePrerequisites = append(servicePrerequisites, base.Ready...)
	workers, err := createService(ctx, serviceArgs{
		Name:                    cfg.Name,
		DesiredCount:            cfg.Scaling.MinTasks,
		Tags:                    tags,
		VPCID:                   vpcNetwork.VPCID,
		SubnetID:                vpcNetwork.PrivateSubnetID,
		EndpointSecurityGroupID: vpcNetwork.EndpointSecurityGroupID,
		S3PrefixListID:          vpcNetwork.S3PrefixListID,
		TaskDefinitionARN:       taskDefinition.ARN,
		Prerequisites:           servicePrerequisites,
	})
	if err != nil {
		return fmt.Errorf("ecs service: %w", err)
	}
	if _, err := autoscaling.New(ctx, cfg.Name, autoscaling.Args{
		ComponentType: "lyrics:autoscaling:Autoscaling",
		Settings:      cfg.Scaling,
		ClusterName:   workers.ClusterName,
		ServiceName:   workers.ServiceName,
		ServiceReady:  workers.ServiceReady,
		QueueName:     base.QueueName,
	}); err != nil {
		return fmt.Errorf("autoscaling: %w", err)
	}
	ctx.Export("repositoryUrl", images.RepositoryURL)
	ctx.Export("clusterName", workers.ClusterName)
	ctx.Export("serviceName", workers.ServiceName)
	return nil
}

// Reuse the current provider identity, and fail before creating any resources
// if the application location disagrees with the effective provider configuration.
func validateLocation(ctx *pulumi.Context, cfg deploymentConfig) error {
	region, err := aws.GetRegion(ctx, nil)
	if err != nil {
		return fmt.Errorf("read AWS provider region: %w", err)
	}
	if region.Region != cfg.Region {
		return fmt.Errorf("aws:region %q differs from AWS provider region %q; check the AWS provider configuration", cfg.Region, region.Region)
	}
	zone, err := aws.GetAvailabilityZone(ctx, &aws.GetAvailabilityZoneArgs{Name: pulumi.StringRef(cfg.AvailabilityZone)})
	if err != nil {
		return fmt.Errorf("vpc.availabilityZone %q: %w", cfg.AvailabilityZone, err)
	}
	if zone.Region != cfg.Region {
		return fmt.Errorf("vpc.availabilityZone %q belongs to %q, expected %q", cfg.AvailabilityZone, zone.Region, cfg.Region)
	}
	return nil
}

func image(reference string, repositoryURL pulumi.StringInput) pulumi.StringInput {
	if reference != "" {
		return pulumi.String(reference)
	}
	const bootstrapTag = "bootstrap"
	return pulumi.Sprintf("%s:%s", repositoryURL, bootstrapTag)
}
