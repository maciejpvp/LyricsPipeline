package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type Network struct {
	VPC           *ec2.Vpc
	PrivateSubnet *ec2.Subnet
}

func NewNetwork(ctx *pulumi.Context, name, availabilityZone string, tags pulumi.StringMap) (*Network, error) {
	vpc, err := ec2.NewVpc(ctx, name+"-vpc", &ec2.VpcArgs{CidrBlock: pulumi.String("10.42.0.0/16"), EnableDnsHostnames: pulumi.Bool(true), EnableDnsSupport: pulumi.Bool(true), Tags: tags})
	if err != nil {
		return nil, err
	}
	publicSubnet, err := ec2.NewSubnet(ctx, name+"-public", &ec2.SubnetArgs{VpcId: vpc.ID(), CidrBlock: pulumi.String("10.42.1.0/24"), AvailabilityZone: pulumi.String(availabilityZone), MapPublicIpOnLaunch: pulumi.Bool(true), Tags: tags})
	if err != nil {
		return nil, err
	}
	privateSubnet, err := ec2.NewSubnet(ctx, name+"-private", &ec2.SubnetArgs{VpcId: vpc.ID(), CidrBlock: pulumi.String("10.42.2.0/24"), AvailabilityZone: pulumi.String(availabilityZone), Tags: tags})
	if err != nil {
		return nil, err
	}
	igw, err := ec2.NewInternetGateway(ctx, name+"-igw", &ec2.InternetGatewayArgs{VpcId: vpc.ID(), Tags: tags})
	if err != nil {
		return nil, err
	}
	publicRoutes, err := ec2.NewRouteTable(ctx, name+"-public-routes", &ec2.RouteTableArgs{VpcId: vpc.ID(), Tags: tags})
	if err != nil {
		return nil, err
	}
	if _, err = ec2.NewRoute(ctx, name+"-internet-route", &ec2.RouteArgs{RouteTableId: publicRoutes.ID(), DestinationCidrBlock: pulumi.String("0.0.0.0/0"), GatewayId: igw.ID()}); err != nil {
		return nil, err
	}
	if _, err = ec2.NewRouteTableAssociation(ctx, name+"-public-association", &ec2.RouteTableAssociationArgs{SubnetId: publicSubnet.ID(), RouteTableId: publicRoutes.ID()}); err != nil {
		return nil, err
	}
	privateRoutes, err := ec2.NewRouteTable(ctx, name+"-private-routes", &ec2.RouteTableArgs{VpcId: vpc.ID(), Tags: tags})
	if err != nil {
		return nil, err
	}
	if _, err = ec2.NewRouteTableAssociation(ctx, name+"-private-association", &ec2.RouteTableAssociationArgs{SubnetId: privateSubnet.ID(), RouteTableId: privateRoutes.ID()}); err != nil {
		return nil, err
	}
	eip, err := ec2.NewEip(ctx, name+"-nat-eip", &ec2.EipArgs{Domain: pulumi.String("vpc"), Tags: tags})
	if err != nil {
		return nil, err
	}
	nat, err := ec2.NewNatGateway(ctx, name+"-nat", &ec2.NatGatewayArgs{AllocationId: eip.ID(), SubnetId: publicSubnet.ID(), Tags: tags})
	if err != nil {
		return nil, err
	}
	if _, err = ec2.NewRoute(ctx, name+"-nat-route", &ec2.RouteArgs{RouteTableId: privateRoutes.ID(), DestinationCidrBlock: pulumi.String("0.0.0.0/0"), NatGatewayId: nat.ID()}); err != nil {
		return nil, err
	}

	return &Network{VPC: vpc, PrivateSubnet: privateSubnet}, nil
}
