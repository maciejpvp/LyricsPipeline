package main

import (
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestCreationReadiness(t *testing.T) {
	m := runDeployment(t, nil)
	for _, edge := range []struct{ child, parent string }{{"nat", "igw"}, {"service", "capacity-providers"}, {"service", "internet-route"}, {"service", "public-association"}, {"service", "private-association"}, {"service", "nat-route"}, {"service", "execution-policy"}, {"service", "task-policy"}} {
		t.Run(edge.child+"_after_"+edge.parent, func(t *testing.T) {
			for _, d := range m.Resources["vocal-extractor-"+edge.child].RegisterRPC.GetDependencies() {
				if resource.URN(d).Name() == "vocal-extractor-"+edge.parent {
					return
				}
			}
			t.Fatal("missing readiness dependency")
		})
	}
	for _, r := range m.AllResources {
		if strings.HasPrefix(r.TypeToken, "aws:s3/") || strings.HasPrefix(r.TypeToken, "aws:sqs/") {
			t.Errorf("separation owns shared storage: %s", r.TypeToken)
		}
	}
}
func TestZoneMismatchBeforeRegistration(t *testing.T) {
	m := &infraMocks{ZoneRegion: "us-east-1"}
	err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, nil))
	if err == nil || len(m.AllResources) != 0 {
		t.Fatalf("zone mismatch accepted: %v", err)
	}
}
