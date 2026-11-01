package ecs

import (
	awsecs "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type ServiceArgs struct {
	DesiredCount      int
	Subnets           pulumi.StringArrayInput
	SecurityGroups    pulumi.StringArrayInput
	CapacityProvider  string
	TaskDefinitionARN pulumi.StringInput
	Prerequisites     []pulumi.Resource
	Tags              pulumi.StringMap
}

type Service struct {
	ClusterName  pulumi.StringOutput
	ServiceName  pulumi.StringOutput
	ServiceReady pulumi.Resource
}

type serviceBuilder struct {
	ctx  *pulumi.Context
	name string
	args ServiceArgs
	opts []pulumi.ResourceOption
}

func NewService(ctx *pulumi.Context, name string, args ServiceArgs, opts ...pulumi.ResourceOption) (*Service, error) {
	result := &Service{}
	b := serviceBuilder{ctx: ctx, name: name, args: args, opts: opts}
	cluster, capacityProviders, err := b.createCluster()
	if err != nil {
		return nil, err
	}
	service, err := b.createService(cluster, capacityProviders)
	if err != nil {
		return nil, err
	}

	result.ClusterName = cluster.Name
	result.ServiceName = service.Name
	result.ServiceReady = service
	return result, nil
}

func (b *serviceBuilder) createCluster() (*awsecs.Cluster, *awsecs.ClusterCapacityProviders, error) {
	cluster, err := awsecs.NewCluster(b.ctx, b.name+"-cluster", &awsecs.ClusterArgs{
		Tags: b.args.Tags,
		Settings: awsecs.ClusterSettingArray{awsecs.ClusterSettingArgs{
			Name:  pulumi.String("containerInsights"),
			Value: pulumi.String("enabled"),
		}},
	}, b.opts...)
	if err != nil {
		return nil, nil, err
	}
	capacityProviders, err := awsecs.NewClusterCapacityProviders(b.ctx, b.name+"-capacity-providers", &awsecs.ClusterCapacityProvidersArgs{
		ClusterName:       cluster.Name,
		CapacityProviders: pulumi.StringArray{pulumi.String(b.args.CapacityProvider)},
	}, b.opts...)
	if err != nil {
		return nil, nil, err
	}

	return cluster, capacityProviders, nil
}

func (b *serviceBuilder) createService(cluster *awsecs.Cluster, capacityProviders *awsecs.ClusterCapacityProviders) (*awsecs.Service, error) {
	dependencies := append([]pulumi.Resource{}, b.args.Prerequisites...)
	dependencies = append(dependencies, capacityProviders)
	service, err := awsecs.NewService(b.ctx, b.name+"-service", &awsecs.ServiceArgs{
		Cluster:        cluster.Arn,
		TaskDefinition: b.args.TaskDefinitionARN,
		DesiredCount:   pulumi.Int(b.args.DesiredCount),
		CapacityProviderStrategies: awsecs.ServiceCapacityProviderStrategyArray{
			awsecs.ServiceCapacityProviderStrategyArgs{
				CapacityProvider: pulumi.String(b.args.CapacityProvider),
				Weight:           pulumi.Int(1),
				Base:             pulumi.Int(0),
			},
		},
		// AWS requires a new deployment when migrating from a launch type.
		ForceNewDeployment: pulumi.Bool(true),
		Triggers:           pulumi.StringMap{"capacityProvider": pulumi.String(b.args.CapacityProvider)},
		NetworkConfiguration: awsecs.ServiceNetworkConfigurationArgs{
			AssignPublicIp: pulumi.Bool(false),
			SecurityGroups: b.args.SecurityGroups,
			Subnets:        b.args.Subnets,
		},
		ForceDelete: pulumi.Bool(true),
		Tags:        b.args.Tags,
	}, append(append([]pulumi.ResourceOption{}, b.opts...), pulumi.DependsOn(dependencies), pulumi.IgnoreChanges([]string{"desiredCount"}))...)
	if err != nil {
		return nil, err
	}
	return service, nil
}
