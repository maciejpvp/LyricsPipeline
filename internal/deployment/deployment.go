// Package deployment composes the infrastructure in dependency order.
package deployment

import (
	"fmt"

	"LyricsPipeline/internal/autoscaling"
	"LyricsPipeline/internal/config"
	"LyricsPipeline/internal/ecr"
	"LyricsPipeline/internal/ecs"
	"LyricsPipeline/internal/iam"
	"LyricsPipeline/internal/ingestion"
	"LyricsPipeline/internal/s3"
	"LyricsPipeline/internal/sqs"
	"LyricsPipeline/internal/vpc"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func Run(ctx *pulumi.Context) error {
	settings, err := config.Load(ctx)
	if err != nil {
		return err
	}
	d := deployment{
		ctx:      ctx,
		settings: settings,
	}
	if err := d.validateLocation(); err != nil {
		return err
	}
	return d.create()
}

type deployment struct {
	ctx      *pulumi.Context
	settings config.Deployment
}

// Reuse the current provider identity, and fail before creating any resources
// if the application location disagrees with the effective provider configuration.
func (d *deployment) validateLocation() error {
	region, err := aws.GetRegion(d.ctx, nil)
	if err != nil {
		return fmt.Errorf("read AWS provider region: %w", err)
	}
	if region.Region != d.settings.Region {
		return fmt.Errorf("aws:region %q differs from AWS provider region %q; check the AWS provider configuration", d.settings.Region, region.Region)
	}
	zone, err := aws.GetAvailabilityZone(d.ctx, &aws.GetAvailabilityZoneArgs{Name: pulumi.StringRef(d.settings.AvailabilityZone)})
	if err != nil {
		return fmt.Errorf("vpc.availabilityZone %q: %w", d.settings.AvailabilityZone, err)
	}
	if zone.Region != d.settings.Region {
		return fmt.Errorf("vpc.availabilityZone %q belongs to %q, expected %q", d.settings.AvailabilityZone, zone.Region, d.settings.Region)
	}
	return nil
}

func (d *deployment) create() error {
	ctx, cfg := d.ctx, d.settings
	tags := pulumi.ToStringMap(cfg.Tags)
	vpcNetwork, err := vpc.New(ctx, cfg.Name, vpc.Args{
		Settings:         cfg.Network,
		AvailabilityZone: cfg.AvailabilityZone,
		Tags:             tags,
	})
	if err != nil {
		return fmt.Errorf("vpc: %w", err)
	}
	media, err := s3.New(ctx, cfg.Name, s3.Args{Tags: tags})
	if err != nil {
		return fmt.Errorf("s3: %w", err)
	}
	jobs, err := sqs.New(ctx, cfg.Name, sqs.Args{
		Settings: cfg.Queue,
		Tags:     tags,
	})
	if err != nil {
		return fmt.Errorf("sqs: %w", err)
	}
	// Retain the original queue parent for policy and notification URNs.
	if _, err := ingestion.New(ctx, cfg.Name, ingestion.Args{
		BucketID:    media.BucketID,
		BucketARN:   media.BucketARN,
		QueueARN:    jobs.ARN,
		QueueURL:    jobs.URL,
		InputPrefix: cfg.Objects.Input,
	}, pulumi.Parent(jobs)); err != nil {
		return fmt.Errorf("ingestion: %w", err)
	}
	roles, err := iam.New(ctx, cfg.Name, iam.Args{
		BucketARN: media.BucketARN,
		QueueARN:  jobs.ARN,
		Objects:   cfg.Objects,
		Tags:      tags,
	})
	if err != nil {
		return fmt.Errorf("iam: %w", err)
	}
	images, err := ecr.New(ctx, cfg.Name, ecr.Args{Tags: tags})
	if err != nil {
		return fmt.Errorf("ecr: %w", err)
	}
	taskDefinition, err := ecs.NewTaskDefinition(ctx, cfg.Name, ecs.TaskDefinitionArgs{
		Image:            d.image(images.RepositoryURL),
		Region:           cfg.Region,
		Settings:         cfg.Task,
		Objects:          cfg.Objects,
		BucketName:       media.BucketName,
		QueueURL:         jobs.URL,
		ExecutionRoleARN: roles.ExecutionRoleARN,
		TaskRoleARN:      roles.TaskRoleARN,
		Tags:             tags,
	})
	if err != nil {
		return fmt.Errorf("ecs task definition: %w", err)
	}
	servicePrerequisites := append([]pulumi.Resource{}, vpcNetwork.RoutingReady...)
	servicePrerequisites = append(servicePrerequisites, roles.PermissionsReady...)
	workers, err := ecs.NewService(ctx, cfg.Name, ecs.ServiceArgs{
		MinTasks:          cfg.Scaling.MinTasks,
		VPCID:             vpcNetwork.VPCID,
		PrivateSubnetID:   vpcNetwork.PrivateSubnetID,
		InternetCIDR:      cfg.Network.Internet,
		TaskDefinitionARN: taskDefinition.ARN,
		Prerequisites:     servicePrerequisites,
		Tags:              tags,
	})
	if err != nil {
		return fmt.Errorf("ecs service: %w", err)
	}
	if _, err := autoscaling.New(ctx, cfg.Name, autoscaling.Args{
		Settings:     cfg.Scaling,
		ClusterName:  workers.ClusterName,
		ServiceName:  workers.ServiceName,
		ServiceReady: workers.ServiceReady,
		QueueName:    jobs.Name,
	}); err != nil {
		return fmt.Errorf("autoscaling: %w", err)
	}
	ctx.Export("mediaBucket", media.BucketName)
	ctx.Export("queueUrl", jobs.URL)
	ctx.Export("queueArn", jobs.ARN)
	ctx.Export("repositoryUrl", images.RepositoryURL)
	ctx.Export("clusterName", workers.ClusterName)
	ctx.Export("serviceName", workers.ServiceName)
	return nil
}

func (d *deployment) image(repositoryURL pulumi.StringInput) pulumi.StringInput {
	if d.settings.Image != "" {
		return pulumi.String(d.settings.Image)
	}
	const bootstrapTag = "bootstrap"
	return pulumi.Sprintf("%s:%s", repositoryURL, bootstrapTag)
}
