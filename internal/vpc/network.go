// Package vpc owns the VPC, subnets and internet routing.
package vpc

import (
	"LyricsPipeline/internal/config"

	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Args struct {
	Settings         config.Network
	AvailabilityZone string
	Tags             pulumi.StringMap
}

type Network struct {
	pulumi.ResourceState
	VPCID           pulumi.IDOutput
	PrivateSubnetID pulumi.IDOutput
	// RoutingReady includes routing associations, not just the subnet's existence.
	RoutingReady []pulumi.Resource
}

type builder struct {
	ctx             *pulumi.Context
	name            string
	args            Args
	parent          pulumi.ResourceOption
	vpc             *ec2.Vpc
	publicSubnet    *ec2.Subnet
	privateSubnet   *ec2.Subnet
	internetGateway *ec2.InternetGateway
	ready           []pulumi.Resource
}

func New(ctx *pulumi.Context, name string, args Args, opts ...pulumi.ResourceOption) (*Network, error) {
	component := &Network{}
	if err := ctx.RegisterComponentResource("lyrics:network:Network", name, component, opts...); err != nil {
		return nil, err
	}
	b := builder{
		ctx:    ctx,
		name:   name,
		args:   args,
		parent: pulumi.Parent(component),
	}
	if err := b.createSubnets(); err != nil {
		return nil, err
	}
	if err := b.createPublicRouting(); err != nil {
		return nil, err
	}
	if err := b.createPrivateRouting(); err != nil {
		return nil, err
	}
	component.VPCID, component.PrivateSubnetID, component.RoutingReady = b.vpc.ID(), b.privateSubnet.ID(), b.ready
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{"vpcId": component.VPCID, "privateSubnetId": component.PrivateSubnetID}); err != nil {
		return nil, err
	}
	return component, nil
}

func (b *builder) createSubnets() error {
	var err error
	b.vpc, err = ec2.NewVpc(b.ctx, b.name+"-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String(b.args.Settings.VPC.String()),
		EnableDnsHostnames: pulumi.Bool(true),
		EnableDnsSupport:   pulumi.Bool(true),
		Tags:               b.args.Tags,
	}, b.parent)
	if err != nil {
		return err
	}
	b.publicSubnet, err = ec2.NewSubnet(b.ctx, b.name+"-public", &ec2.SubnetArgs{
		VpcId:               b.vpc.ID(),
		CidrBlock:           pulumi.String(b.args.Settings.PublicSubnet.String()),
		AvailabilityZone:    pulumi.String(b.args.AvailabilityZone),
		MapPublicIpOnLaunch: pulumi.Bool(true),
		Tags:                b.args.Tags,
	}, b.parent)
	if err != nil {
		return err
	}
	b.privateSubnet, err = ec2.NewSubnet(b.ctx, b.name+"-private", &ec2.SubnetArgs{
		VpcId:            b.vpc.ID(),
		CidrBlock:        pulumi.String(b.args.Settings.PrivateSubnet.String()),
		AvailabilityZone: pulumi.String(b.args.AvailabilityZone),
		Tags:             b.args.Tags,
	}, b.parent)
	return err
}

func (b *builder) createPublicRouting() error {
	var err error
	b.internetGateway, err = ec2.NewInternetGateway(b.ctx, b.name+"-igw", &ec2.InternetGatewayArgs{
		VpcId: b.vpc.ID(),
		Tags:  b.args.Tags,
	}, b.parent)
	if err != nil {
		return err
	}
	table, err := ec2.NewRouteTable(b.ctx, b.name+"-public-routes", &ec2.RouteTableArgs{
		VpcId: b.vpc.ID(),
		Tags:  b.args.Tags,
	}, b.parent)
	if err != nil {
		return err
	}
	route, err := ec2.NewRoute(b.ctx, b.name+"-internet-route", &ec2.RouteArgs{
		RouteTableId:         table.ID(),
		DestinationCidrBlock: pulumi.String(b.args.Settings.Internet.String()),
		GatewayId:            b.internetGateway.ID(),
	}, b.parent)
	if err != nil {
		return err
	}
	b.ready = append(b.ready, route)
	return b.associateRouteTable(b.name+"-public-association", b.publicSubnet, table)
}

func (b *builder) createPrivateRouting() error {
	table, err := ec2.NewRouteTable(b.ctx, b.name+"-private-routes", &ec2.RouteTableArgs{
		VpcId: b.vpc.ID(),
		Tags:  b.args.Tags,
	}, b.parent)
	if err != nil {
		return err
	}
	if err = b.associateRouteTable(b.name+"-private-association", b.privateSubnet, table); err != nil {
		return err
	}
	eip, err := ec2.NewEip(b.ctx, b.name+"-nat-eip", &ec2.EipArgs{
		Domain: pulumi.String("vpc"),
		Tags:   b.args.Tags,
	}, b.parent)
	if err != nil {
		return err
	}
	nat, err := ec2.NewNatGateway(b.ctx, b.name+"-nat", &ec2.NatGatewayArgs{
		AllocationId: eip.ID(),
		SubnetId:     b.publicSubnet.ID(),
		Tags:         b.args.Tags,
	},
		b.parent, pulumi.DependsOn([]pulumi.Resource{b.internetGateway}))
	if err != nil {
		return err
	}
	route, err := ec2.NewRoute(b.ctx, b.name+"-nat-route", &ec2.RouteArgs{
		RouteTableId:         table.ID(),
		DestinationCidrBlock: pulumi.String(b.args.Settings.Internet.String()),
		NatGatewayId:         nat.ID(),
	}, b.parent)
	if err != nil {
		return err
	}
	b.ready = append(b.ready, route)
	return nil
}

func (b *builder) associateRouteTable(name string, subnet *ec2.Subnet, table *ec2.RouteTable) error {
	association, err := ec2.NewRouteTableAssociation(b.ctx, name, &ec2.RouteTableAssociationArgs{
		SubnetId:     subnet.ID(),
		RouteTableId: table.ID(),
	}, b.parent)
	if err != nil {
		return err
	}
	b.ready = append(b.ready, association)
	return nil
}
