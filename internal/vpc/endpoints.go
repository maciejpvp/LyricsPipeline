package vpc

import (
	"strings"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func (b *builder) createEndpointGroup() error {
	var err error
	b.endpointGroup, err = ec2.NewSecurityGroup(b.ctx, b.name+"-endpoints", &ec2.SecurityGroupArgs{
		VpcId: b.vpc.ID(), Description: pulumi.String("Private service endpoints"),
		// Ingress is owned exclusively by the caller's standalone worker rule.
		// Stateful replies need no outbound rules.
		Egress: ec2.SecurityGroupEgressArray{}, Tags: b.args.Tags,
	}, b.parent)
	return err
}

func (b *builder) createS3Endpoint() error {
	var err error
	b.s3Endpoint, err = ec2.NewVpcEndpoint(b.ctx, b.name+"-s3-endpoint", &ec2.VpcEndpointArgs{
		VpcId: b.vpc.ID(), ServiceName: pulumi.String("com.amazonaws." + b.args.Region + ".s3"),
		VpcEndpointType: pulumi.String("Gateway"), RouteTableIds: pulumi.StringArray{b.routeTable.ID()},
		// The default endpoint policy also permits ECR's regional layer bucket.
		// Existing task/execution IAM policies remain the authorization boundary.
		Tags: b.args.Tags,
	}, b.parent)
	if err != nil {
		return err
	}
	b.ready = append(b.ready, b.s3Endpoint)
	return nil
}

func (b *builder) createInterfaceEndpoints() error {
	for _, service := range b.args.InterfaceServices {
		endpoint, err := ec2.NewVpcEndpoint(b.ctx, b.name+"-"+strings.ReplaceAll(service, ".", "-")+"-endpoint", &ec2.VpcEndpointArgs{
			VpcId: b.vpc.ID(), ServiceName: pulumi.String("com.amazonaws." + b.args.Region + "." + service),
			VpcEndpointType: pulumi.String("Interface"), PrivateDnsEnabled: pulumi.Bool(true),
			SubnetIds:        pulumi.StringArray{b.privateSubnet.ID()},
			SecurityGroupIds: pulumi.StringArray{b.endpointGroup.ID()}, Tags: b.args.Tags,
		}, b.parent)
		if err != nil {
			return err
		}
		b.ready = append(b.ready, endpoint)
	}
	return nil
}
