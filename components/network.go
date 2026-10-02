package components

import (
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/ec2"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// NetworkConfig contains the values used to build a network and its routing
// layers. Keeping these values together makes the network layout easy to
// review and change without searching through resource declarations.
type NetworkConfig struct {
	Name             string
	AvailabilityZone string
	Tags             pulumi.StringMap

	VPCCIDR           string
	PublicSubnetCIDR  string
	PrivateSubnetCIDR string
	InternetCIDR      string
}

type Network struct {
	pulumi.ResourceState
	VPC           *ec2.Vpc
	PrivateSubnet *ec2.Subnet
}

type networkResources struct {
	vpc           *ec2.Vpc
	publicSubnet  *ec2.Subnet
	privateSubnet *ec2.Subnet
}

func NewNetwork(ctx *pulumi.Context, config NetworkConfig, opts ...pulumi.ResourceOption) (*Network, error) {
	component := &Network{}
	if err := ctx.RegisterComponentResource("lyrics:network:Network", config.Name, component, opts...); err != nil {
		return nil, err
	}

	parent := pulumi.Parent(component)
	resources, err := createNetworkResources(ctx, config, parent)
	if err != nil {
		return nil, err
	}
	if err := createPublicRouting(ctx, config, resources, parent); err != nil {
		return nil, err
	}
	if err := createPrivateRouting(ctx, config, resources, parent); err != nil {
		return nil, err
	}

	component.VPC = resources.vpc
	component.PrivateSubnet = resources.privateSubnet
	if err := ctx.RegisterResourceOutputs(component, pulumi.Map{
		"vpcId":           resources.vpc.ID(),
		"privateSubnetId": resources.privateSubnet.ID(),
	}); err != nil {
		return nil, err
	}

	return component, nil
}

func createNetworkResources(ctx *pulumi.Context, config NetworkConfig, parent pulumi.ResourceOption) (*networkResources, error) {
	vpc, err := ec2.NewVpc(ctx, config.Name+"-vpc", &ec2.VpcArgs{
		CidrBlock:          pulumi.String(config.VPCCIDR),
		EnableDnsHostnames: pulumi.Bool(true),
		EnableDnsSupport:   pulumi.Bool(true),
		Tags:               config.Tags,
	}, parent)
	if err != nil {
		return nil, err
	}

	publicSubnet, err := ec2.NewSubnet(ctx, config.Name+"-public", &ec2.SubnetArgs{
		VpcId:               vpc.ID(),
		CidrBlock:           pulumi.String(config.PublicSubnetCIDR),
		AvailabilityZone:    pulumi.String(config.AvailabilityZone),
		MapPublicIpOnLaunch: pulumi.Bool(true),
		Tags:                config.Tags,
	}, parent)
	if err != nil {
		return nil, err
	}

	privateSubnet, err := ec2.NewSubnet(ctx, config.Name+"-private", &ec2.SubnetArgs{
		VpcId:            vpc.ID(),
		CidrBlock:        pulumi.String(config.PrivateSubnetCIDR),
		AvailabilityZone: pulumi.String(config.AvailabilityZone),
		Tags:             config.Tags,
	}, parent)
	if err != nil {
		return nil, err
	}

	return &networkResources{vpc: vpc, publicSubnet: publicSubnet, privateSubnet: privateSubnet}, nil
}

func createPublicRouting(ctx *pulumi.Context, config NetworkConfig, resources *networkResources, parent pulumi.ResourceOption) error {
	internetGateway, err := ec2.NewInternetGateway(ctx, config.Name+"-igw", &ec2.InternetGatewayArgs{
		VpcId: resources.vpc.ID(),
		Tags:  config.Tags,
	}, parent)
	if err != nil {
		return err
	}

	routeTable, err := ec2.NewRouteTable(ctx, config.Name+"-public-routes", &ec2.RouteTableArgs{
		VpcId: resources.vpc.ID(),
		Tags:  config.Tags,
	}, parent)
	if err != nil {
		return err
	}

	if _, err = ec2.NewRoute(ctx, config.Name+"-internet-route", &ec2.RouteArgs{
		RouteTableId:         routeTable.ID(),
		DestinationCidrBlock: pulumi.String(config.InternetCIDR),
		GatewayId:            internetGateway.ID(),
	}, parent); err != nil {
		return err
	}

	return associateRouteTable(ctx, config.Name+"-public-association", resources.publicSubnet, routeTable, parent)
}

func createPrivateRouting(ctx *pulumi.Context, config NetworkConfig, resources *networkResources, parent pulumi.ResourceOption) error {
	routeTable, err := ec2.NewRouteTable(ctx, config.Name+"-private-routes", &ec2.RouteTableArgs{
		VpcId: resources.vpc.ID(),
		Tags:  config.Tags,
	}, parent)
	if err != nil {
		return err
	}

	if err := associateRouteTable(ctx, config.Name+"-private-association", resources.privateSubnet, routeTable, parent); err != nil {
		return err
	}

	eip, err := ec2.NewEip(ctx, config.Name+"-nat-eip", &ec2.EipArgs{
		Domain: pulumi.String("vpc"),
		Tags:   config.Tags,
	}, parent)
	if err != nil {
		return err
	}

	natGateway, err := ec2.NewNatGateway(ctx, config.Name+"-nat", &ec2.NatGatewayArgs{
		AllocationId: eip.ID(),
		SubnetId:     resources.publicSubnet.ID(),
		Tags:         config.Tags,
	}, parent)
	if err != nil {
		return err
	}

	_, err = ec2.NewRoute(ctx, config.Name+"-nat-route", &ec2.RouteArgs{
		RouteTableId:         routeTable.ID(),
		DestinationCidrBlock: pulumi.String(config.InternetCIDR),
		NatGatewayId:         natGateway.ID(),
	}, parent)
	return err
}

func associateRouteTable(ctx *pulumi.Context, name string, subnet *ec2.Subnet, routeTable *ec2.RouteTable, parent pulumi.ResourceOption) error {
	_, err := ec2.NewRouteTableAssociation(ctx, name, &ec2.RouteTableAssociationArgs{
		SubnetId:     subnet.ID(),
		RouteTableId: routeTable.ID(),
	}, parent)
	return err
}
