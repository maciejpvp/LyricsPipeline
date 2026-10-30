package ecs

import (
	"net/netip"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	awsecs "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

const spotCapacityProvider = "FARGATE_SPOT"
const allProtocols = "-1"

type ServiceArgs struct {
	MinTasks          int
	VPCID             pulumi.IDInput
	PrivateSubnetID   pulumi.IDInput
	InternetCIDR      netip.Prefix
	TaskDefinitionARN pulumi.StringInput
	Prerequisites     []pulumi.Resource
	Tags              pulumi.StringMap
}

type Service struct {
	pulumi.ResourceState
	ClusterName  pulumi.StringOutput
	ServiceName  pulumi.StringOutput
	ServiceReady pulumi.Resource
}

type serviceBuilder struct {
	ctx    *pulumi.Context
	name   string
	args   ServiceArgs
	parent pulumi.ResourceOption
}

func NewService(ctx *pulumi.Context, name string, args ServiceArgs, opts ...pulumi.ResourceOption) (*Service, error) {
	component := &Service{}
	if err := ctx.RegisterComponentResource("lyrics:worker:Worker", name, component, opts...); err != nil {
		return nil, err
	}
	b := serviceBuilder{ctx: ctx, name: name, args: args, parent: pulumi.Parent(component)}
	cluster, capacityProviders, err := b.createCluster()
	if err != nil {
		return nil, err
	}
	securityGroup, err := b.createSecurityGroup()
	if err != nil {
		return nil, err
	}
	service, err := b.createService(cluster, capacityProviders, securityGroup)
	if err != nil {
		return nil, err
	}

	component.ClusterName = cluster.Name
	component.ServiceName = service.Name
	component.ServiceReady = service
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"clusterName": cluster.Name,
		"serviceName": service.Name,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

func (b *serviceBuilder) createCluster() (*awsecs.Cluster, *awsecs.ClusterCapacityProviders, error) {
	cluster, err := awsecs.NewCluster(b.ctx, b.name+"-cluster", &awsecs.ClusterArgs{
		Tags: b.args.Tags,
		Settings: awsecs.ClusterSettingArray{awsecs.ClusterSettingArgs{
			Name:  pulumi.String("containerInsights"),
			Value: pulumi.String("enabled"),
		}},
	}, b.parent)
	if err != nil {
		return nil, nil, err
	}
	capacityProviders, err := awsecs.NewClusterCapacityProviders(b.ctx, b.name+"-capacity-providers", &awsecs.ClusterCapacityProvidersArgs{
		ClusterName:       cluster.Name,
		CapacityProviders: pulumi.StringArray{pulumi.String(spotCapacityProvider)},
	}, b.parent)
	if err != nil {
		return nil, nil, err
	}

	return cluster, capacityProviders, nil
}

func (b *serviceBuilder) createSecurityGroup() (*ec2.SecurityGroup, error) {
	securityGroup, err := ec2.NewSecurityGroup(b.ctx, b.name+"-tasks", &ec2.SecurityGroupArgs{
		VpcId:       b.args.VPCID.ToIDOutput().ToStringPtrOutput(),
		Description: pulumi.String("Fargate worker egress"),
		Egress: ec2.SecurityGroupEgressArray{ec2.SecurityGroupEgressArgs{
			Protocol:   pulumi.String(allProtocols),
			FromPort:   pulumi.Int(0),
			ToPort:     pulumi.Int(0),
			CidrBlocks: pulumi.StringArray{pulumi.String(b.args.InternetCIDR.String())},
		}},
		Tags: b.args.Tags,
	}, b.parent)
	if err != nil {
		return nil, err
	}

	return securityGroup, nil
}

func (b *serviceBuilder) createService(cluster *awsecs.Cluster, capacityProviders *awsecs.ClusterCapacityProviders, securityGroup *ec2.SecurityGroup) (*awsecs.Service, error) {
	dependencies := append([]pulumi.Resource{}, b.args.Prerequisites...)
	dependencies = append(dependencies, capacityProviders)
	service, err := awsecs.NewService(b.ctx, b.name+"-service", &awsecs.ServiceArgs{
		Cluster:        cluster.Arn,
		TaskDefinition: b.args.TaskDefinitionARN,
		DesiredCount:   pulumi.Int(b.args.MinTasks),
		CapacityProviderStrategies: awsecs.ServiceCapacityProviderStrategyArray{
			awsecs.ServiceCapacityProviderStrategyArgs{
				CapacityProvider: pulumi.String(spotCapacityProvider),
				Weight:           pulumi.Int(1),
				Base:             pulumi.Int(0),
			},
		},
		// AWS requires a new deployment when migrating from a launch type.
		ForceNewDeployment: pulumi.Bool(true),
		Triggers:           pulumi.StringMap{"capacityProvider": pulumi.String(spotCapacityProvider)},
		NetworkConfiguration: awsecs.ServiceNetworkConfigurationArgs{
			AssignPublicIp: pulumi.Bool(false),
			SecurityGroups: pulumi.StringArray{securityGroup.ID()},
			Subnets:        pulumi.StringArray{b.args.PrivateSubnetID.ToIDOutput().ToStringOutput()},
		},
		ForceDelete: pulumi.Bool(true),
		Tags:        b.args.Tags,
	}, b.parent, pulumi.DependsOn(dependencies), pulumi.IgnoreChanges([]string{"desiredCount"}))
	if err != nil {
		return nil, err
	}
	return service, nil
}
