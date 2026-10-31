package main

import (
	"strings"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
)

func TestBaseOwnership(t *testing.T) {
	m := runDeployment(t, map[string]string{"s3": `{"inputPrefix":"uploads/audio","outputPrefix":"results/stems"}`})
	notifications := 0
	for _, r := range m.AllResources {
		if strings.Contains(r.TypeToken, "bucketNotification") {
			notifications++
			if !r.Inputs["eventbridge"].BoolValue() {
				t.Error("EventBridge disabled")
			}
			if r.Inputs["queues"].ArrayValue()[0].ObjectValue()["filterPrefix"].StringValue() != "uploads/audio/" {
				t.Error("input filter changed")
			}
			found := false
			for _, d := range r.RegisterRPC.GetDependencies() {
				if resource.URN(d).Name() == "vocal-extractor-queue-policy" {
					found = true
				}
			}
			if !found {
				t.Error("notification must wait for queue permissions")
			}
		}
		for _, forbidden := range []string{"aws:ec2/", "aws:ecs/", "aws:ecr/", "aws:lambda/", "aws:sfn/"} {
			if strings.HasPrefix(r.TypeToken, forbidden) {
				t.Errorf("base owns %s", r.TypeToken)
			}
		}
	}
	if notifications != 1 {
		t.Fatalf("notification writers=%d", notifications)
	}
}
