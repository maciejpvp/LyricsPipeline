// Package vpc owns private subnets, routing and VPC endpoints.
package vpc

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Args struct {
	ComponentType     string
	Settings          Settings
	Region            string
	AvailabilityZone  string
	InterfaceServices []string
	Tags              pulumi.StringMap
}

type Network struct {
	pulumi.ResourceState
	VPCID                   pulumi.IDOutput
	PrivateSubnetID         pulumi.IDOutput
	EndpointSecurityGroupID pulumi.IDOutput
	S3PrefixListID          pulumi.StringOutput
	// ConnectivityReady includes route associations and all endpoint resources.
	ConnectivityReady []pulumi.Resource
}

type builder struct {
	ctx           *pulumi.Context
	name          string
	args          Args
	parent        pulumi.ResourceOption
	vpc           *ec2.Vpc
	privateSubnet *ec2.Subnet
	routeTable    *ec2.RouteTable
	endpointGroup *ec2.SecurityGroup
	s3Endpoint    *ec2.VpcEndpoint
	ready         []pulumi.Resource
}

func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Network, error) {
	component := &Network{}
	if err := ctx.RegisterComponentResource(args.ComponentType, name, component, opts...); err != nil {
		return nil, err
	}
	b := builder{ctx: ctx, name: name, args: args, parent: pulumi.Parent(component)}
	for _, create := range []func() error{
		b.createVPC, b.createPrivateSubnet, b.createPrivateRouting,
		b.createEndpointGroup, b.createS3Endpoint, b.createInterfaceEndpoints,
	} {
		if err := create(); err != nil {
			return nil, err
		}
	}
	component.VPCID = b.vpc.ID()
	component.PrivateSubnetID = b.privateSubnet.ID()
	component.EndpointSecurityGroupID = b.endpointGroup.ID()
	component.S3PrefixListID = b.s3Endpoint.PrefixListId
	component.ConnectivityReady = b.ready
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"vpcId": component.VPCID, "privateSubnetId": component.PrivateSubnetID,
		"endpointSecurityGroupId": component.EndpointSecurityGroupID, "s3PrefixListId": component.S3PrefixListID,
	}); err != nil {
		return nil, err
	}
	return component, nil
}

func (b *builder) createVPC() error {
	var err error
	b.vpc, err = ec2.NewVpc(b.ctx, b.name+"-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String(b.args.Settings.VPC.String()),
		EnableDnsHostnames: pulumi.Bool(true),
		EnableDnsSupport:   pulumi.Bool(true),
		Tags:               b.args.Tags,
	}, b.parent)
	return err
}

func (b *builder) createPrivateSubnet() error {
	var err error
	b.privateSubnet, err = ec2.NewSubnet(b.ctx, b.name+"-private", &ec2.SubnetArgs{
		VpcId:               b.vpc.ID(),
		CidrBlock:           pulumi.String(b.args.Settings.PrivateSubnet.String()),
		AvailabilityZone:    pulumi.String(b.args.AvailabilityZone),
		MapPublicIpOnLaunch: pulumi.Bool(false),
		Tags:                b.args.Tags,
	}, b.parent)
	return err
}

func (b *builder) createPrivateRouting() error {
	var err error
	b.routeTable, err = ec2.NewRouteTable(b.ctx, b.name+"-private-routes", &ec2.RouteTableArgs{
		VpcId: b.vpc.ID(), Tags: b.args.Tags,
	}, b.parent)
	if err != nil {
		return err
	}
	association, err := ec2.NewRouteTableAssociation(b.ctx, b.name+"-private-association", &ec2.RouteTableAssociationArgs{
		SubnetId: b.privateSubnet.ID(), RouteTableId: b.routeTable.ID(),
	}, b.parent)
	if err != nil {
		return err
	}
	b.ready = append(b.ready, association)
	return nil
}
