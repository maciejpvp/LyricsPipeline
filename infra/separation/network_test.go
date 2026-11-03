package main

import (
	"reflect"
	"strings"
	"testing"

	"LyricsPipeline/internal/infratest"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func workerEndpointEgress() []any {
	return []any{
		map[string]any{"protocol": "tcp", "fromPort": float64(443), "toPort": float64(443), "securityGroups": []any{"vocal-extractor-endpoints-id"}},
		map[string]any{"protocol": "tcp", "fromPort": float64(443), "toPort": float64(443), "prefixListIds": []any{"pl-0123456789abcdef0"}},
	}
}

func TestWorkerPrivateNetworking(t *testing.T) {
	m := runDeployment(t, map[string]string{"vpc": `{"availabilityZone":"eu-central-1b","privateSubnetCIDR":"10.42.3.0/24"}`})
	network := m.Resources["vocal-extractor-service"].Inputs["networkConfiguration"].ObjectValue()
	if network["assignPublicIp"].BoolValue() {
		t.Error("private workers must not receive a public IPv4 address")
	}
	if got := network["subnets"].ArrayValue(); len(got) != 1 || got[0].StringValue() != "vocal-extractor-private-id" {
		t.Fatalf("worker must use only the private subnet: %v", got)
	}
	if got := network["securityGroups"].ArrayValue(); len(got) != 1 || got[0].StringValue() != "vocal-extractor-tasks-id" {
		t.Fatalf("unexpected worker security groups: %v", got)
	}
	subnet := m.Resources["vocal-extractor-private"].Inputs
	if subnet["cidrBlock"].StringValue() != "10.42.3.0/24" || subnet["availabilityZone"].StringValue() != "eu-central-1b" || subnet["mapPublicIpOnLaunch"].BoolValue() {
		t.Error("private subnet must honor configured CIDR/AZ and disable automatic public IPs")
	}
	vpc := m.Resources["vocal-extractor-vpc"].Inputs
	if !vpc["enableDnsSupport"].BoolValue() || !vpc["enableDnsHostnames"].BoolValue() {
		t.Error("private endpoint DNS requires VPC DNS support and hostnames")
	}
	group := m.Resources["vocal-extractor-tasks"].Inputs
	if ingress, ok := group["ingress"]; !ok || !ingress.IsArray() || len(ingress.ArrayValue()) != 0 {
		t.Fatal("workers must explicitly manage an empty ingress list")
	}
	if got := group.Mappable()["egress"]; !reflect.DeepEqual(got, workerEndpointEgress()) {
		t.Errorf("worker egress must target only endpoints and S3: got %#v", got)
	}
	endpoints := m.Resources["vocal-extractor-endpoints"].Inputs
	if _, ok := endpoints["ingress"]; ok {
		t.Error("endpoint ingress must be owned only by the standalone rule")
	}
	if egress, ok := endpoints["egress"]; !ok || len(egress.ArrayValue()) != 0 {
		t.Error("endpoint group must not initiate outbound connections")
	}
	ingress := m.Resources["vocal-extractor-endpoints-ingress"].Inputs.Mappable()
	wantIngress := map[string]any{
		"type": "ingress", "protocol": "tcp", "fromPort": float64(443), "toPort": float64(443),
		"securityGroupId": "vocal-extractor-endpoints-id", "sourceSecurityGroupId": "vocal-extractor-tasks-id",
	}
	if !reflect.DeepEqual(ingress, wantIngress) {
		t.Errorf("only workers may open HTTPS connections to endpoints: got %#v", ingress)
	}

	endpointCount := 0
	for _, r := range m.AllResources {
		switch r.TypeToken {
		case "aws:ec2/vpcEndpoint:VpcEndpoint":
			endpointCount++
		case "aws:ec2/natGateway:NatGateway", "aws:ec2/eip:Eip", "aws:ec2/internetGateway:InternetGateway", "aws:ec2/route:Route":
			t.Errorf("unexpected internet resource: %s %s", r.TypeToken, r.Name)
		}
		if strings.Contains(r.Name, "-public") || strings.Contains(r.Name, "-nat") {
			t.Errorf("unexpected public/NAT resource: %s", r.Name)
		}
	}
	if endpointCount != 5 {
		t.Fatalf("expected one gateway and four interface endpoints, got %d", endpointCount)
	}
}

func TestPrivateServiceEndpoints(t *testing.T) {
	m := &infraMocks{ReferenceOutputs: baseOutputs(), ProviderRegion: "eu-west-1"}
	m.ReferenceOutputs["region"] = resource.NewStringProperty("eu-west-1")
	m.ReferenceOutputs["queueArn"] = resource.NewStringProperty("arn:aws:sqs:eu-west-1:123456789012:jobs")
	if err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, map[string]string{
		"aws:region": "eu-west-1", "vpc": `{"availabilityZone":"eu-west-1a"}`,
	})); err != nil {
		t.Fatal(err)
	}
	for _, service := range []string{"s3", "ecr.api", "ecr.dkr", "sqs", "logs"} {
		t.Run(service, func(t *testing.T) {
			name := "vocal-extractor-" + strings.ReplaceAll(service, ".", "-") + "-endpoint"
			inputs := m.Resources[name].Inputs.Mappable()
			want := map[string]any{
				"vpcId": "vocal-extractor-vpc-id", "serviceName": "com.amazonaws.eu-west-1." + service,
				"tags": map[string]any{"Purpose": "vocal-extractor"},
			}
			if service == "s3" {
				want["vpcEndpointType"] = "Gateway"
				want["routeTableIds"] = []any{"vocal-extractor-private-routes-id"}
			} else {
				want["vpcEndpointType"] = "Interface"
				want["privateDnsEnabled"] = true
				want["subnetIds"] = []any{"vocal-extractor-private-id"}
				want["securityGroupIds"] = []any{"vocal-extractor-endpoints-id"}
			}
			if !reflect.DeepEqual(inputs, want) {
				t.Errorf("unexpected endpoint configuration: got %#v want %#v", inputs, want)
			}
		})
	}
}

// New network resources have explicit expected identities, parents and inputs.
// Existing application resources remain covered by the historical fixture.
func endpointSnapshots() []infratest.ResourceSnapshot {
	const networkParent = "urn:pulumi:test::LyricsPipeline-Separation::lyrics:network:Network::vocal-extractor"
	const workerParent = "urn:pulumi:test::LyricsPipeline-Separation::lyrics:worker:Worker::vocal-extractor"
	networkURN := func(token, name string) string {
		return "urn:pulumi:test::LyricsPipeline-Separation::lyrics:network:Network$" + token + "::vocal-extractor-" + name
	}
	vpcURN := networkURN("aws:ec2/vpc:Vpc", "vpc")
	groupURN := networkURN("aws:ec2/securityGroup:SecurityGroup", "endpoints")
	result := []infratest.ResourceSnapshot{{
		Type: "aws:ec2/securityGroup:SecurityGroup", Name: "vocal-extractor-endpoints", Parent: networkParent,
		Inputs:       map[string]any{"vpcId": "vocal-extractor-vpc-id", "description": "Private service endpoints", "egress": []any{}, "tags": map[string]any{"Purpose": "vocal-extractor"}},
		Dependencies: []string{vpcURN},
	}, {
		Type: "aws:ec2/securityGroupRule:SecurityGroupRule", Name: "vocal-extractor-endpoints-ingress", Parent: workerParent,
		Inputs:       map[string]any{"type": "ingress", "protocol": "tcp", "fromPort": float64(443), "toPort": float64(443), "securityGroupId": "vocal-extractor-endpoints-id", "sourceSecurityGroupId": "vocal-extractor-tasks-id"},
		Dependencies: []string{groupURN, "urn:pulumi:test::LyricsPipeline-Separation::lyrics:worker:Worker$aws:ec2/securityGroup:SecurityGroup::vocal-extractor-tasks"},
	}}
	for _, service := range []string{"s3", "ecr.api", "ecr.dkr", "sqs", "logs"} {
		snapshot := infratest.ResourceSnapshot{
			Type: "aws:ec2/vpcEndpoint:VpcEndpoint", Name: "vocal-extractor-" + strings.ReplaceAll(service, ".", "-") + "-endpoint", Parent: networkParent,
			Inputs:       map[string]any{"vpcId": "vocal-extractor-vpc-id", "serviceName": "com.amazonaws.eu-central-1." + service, "tags": map[string]any{"Purpose": "vocal-extractor"}},
			Dependencies: []string{vpcURN},
		}
		if service == "s3" {
			snapshot.Inputs["vpcEndpointType"] = "Gateway"
			snapshot.Inputs["routeTableIds"] = []any{"vocal-extractor-private-routes-id"}
			snapshot.Dependencies = append(snapshot.Dependencies, networkURN("aws:ec2/routeTable:RouteTable", "private-routes"))
		} else {
			snapshot.Inputs["vpcEndpointType"] = "Interface"
			snapshot.Inputs["privateDnsEnabled"] = true
			snapshot.Inputs["subnetIds"] = []any{"vocal-extractor-private-id"}
			snapshot.Inputs["securityGroupIds"] = []any{"vocal-extractor-endpoints-id"}
			snapshot.Dependencies = append(snapshot.Dependencies, groupURN, networkURN("aws:ec2/subnet:Subnet", "private"))
		}
		result = append(result, snapshot)
	}
	return result
}
