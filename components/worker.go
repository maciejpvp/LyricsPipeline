package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Worker struct {
	pulumi.ResourceState
	Cluster *ecs.Cluster
	Service *ecs.Service
}

func NewWorker(ctx *pulumi.Context, name string, minTasks int, network *Network, image *Image, tags pulumi.StringMap, opts ...pulumi.ResourceOption) (*Worker, error) {
	component := &Worker{}
	if err := ctx.RegisterComponentResource("lyrics:worker:Worker", name, component, opts...); err != nil {
		return nil, err
	}
	parent := pulumi.Parent(component)
	cluster, err := ecs.NewCluster(ctx, name+"-cluster", &ecs.ClusterArgs{
		Tags: tags,
		Settings: ecs.ClusterSettingArray{ecs.ClusterSettingArgs{
			Name:  pulumi.String("containerInsights"),
			Value: pulumi.String("enabled"),
		}},
	}, parent)
	if err != nil {
		return nil, err
	}
	capacityProviders, err := ecs.NewClusterCapacityProviders(ctx, name+"-capacity-providers", &ecs.ClusterCapacityProvidersArgs{
		ClusterName:       cluster.Name,
		CapacityProviders: pulumi.StringArray{pulumi.String("FARGATE_SPOT")},
	}, parent)
	if err != nil {
		return nil, err
	}
	securityGroup, err := ec2.NewSecurityGroup(ctx, name+"-tasks", &ec2.SecurityGroupArgs{
		VpcId:       network.VPC.ID(),
		Description: pulumi.String("Fargate worker egress"),
		Egress: ec2.SecurityGroupEgressArray{ec2.SecurityGroupEgressArgs{
			Protocol:   pulumi.String("-1"),
			FromPort:   pulumi.Int(0),
			ToPort:     pulumi.Int(0),
			CidrBlocks: pulumi.StringArray{pulumi.String("0.0.0.0/0")},
		}},
		Tags: tags,
	}, parent)
	if err != nil {
		return nil, err
	}

	service, err := ecs.NewService(ctx, name+"-service", &ecs.ServiceArgs{
		Cluster:        cluster.Arn,
		TaskDefinition: image.TaskDefinition.Arn,
		DesiredCount:   pulumi.Int(minTasks),
		CapacityProviderStrategies: ecs.ServiceCapacityProviderStrategyArray{
			ecs.ServiceCapacityProviderStrategyArgs{
				CapacityProvider: pulumi.String("FARGATE_SPOT"),
				Weight:           pulumi.Int(1),
				Base:             pulumi.Int(0),
			},
		},
		// AWS requires a new deployment when migrating from a launch type.
		ForceNewDeployment: pulumi.Bool(true),
		Triggers:           pulumi.StringMap{"capacityProvider": pulumi.String("FARGATE_SPOT")},
		NetworkConfiguration: ecs.ServiceNetworkConfigurationArgs{
			AssignPublicIp: pulumi.Bool(false),
			SecurityGroups: pulumi.StringArray{securityGroup.ID()},
			Subnets:        pulumi.StringArray{network.PrivateSubnet.ID()},
		},
		ForceDelete: pulumi.Bool(true),
		Tags:        tags,
	}, parent, pulumi.DependsOn([]pulumi.Resource{capacityProviders}), pulumi.IgnoreChanges([]string{"desiredCount"}))
	if err != nil {
		return nil, err
	}
	component.Cluster = cluster
	component.Service = service
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"clusterName": cluster.Name,
		"serviceName": service.Name,
	}); err != nil {
		return nil, err
	}
	return component, nil
}
