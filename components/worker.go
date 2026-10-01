package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ecs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Worker struct {
	Cluster *ecs.Cluster
	Service *ecs.Service
}

func NewWorker(ctx *pulumi.Context, name string, network *Network, image *Image, tags pulumi.StringMap) (*Worker, error) {
	cluster, err := ecs.NewCluster(ctx, name+"-cluster", &ecs.ClusterArgs{Tags: tags})
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
	})
	if err != nil {
		return nil, err
	}

	service, err := ecs.NewService(ctx, name+"-service", &ecs.ServiceArgs{
		Cluster:        cluster.Arn,
		TaskDefinition: image.TaskDefinition.Arn,
		DesiredCount:   pulumi.Int(0),
		LaunchType:     pulumi.String("FARGATE"),
		NetworkConfiguration: ecs.ServiceNetworkConfigurationArgs{
			AssignPublicIp: pulumi.Bool(false),
			SecurityGroups: pulumi.StringArray{securityGroup.ID()},
			Subnets:        pulumi.StringArray{network.PrivateSubnet.ID()},
		},
		ForceDelete: pulumi.Bool(true),
		Tags:        tags,
	})
	if err != nil {
		return nil, err
	}
	return &Worker{Cluster: cluster, Service: service}, nil
}
