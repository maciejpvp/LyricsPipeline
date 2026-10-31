package main

import (
	"encoding/json"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"LyricsPipeline/internal/configtest"
	"LyricsPipeline/internal/infratest"
	"LyricsPipeline/internal/vpc"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func nativeValues(t *testing.T) map[string]string { return configtest.ValuesAt(t, "Pulumi.yaml") }
func loadForTest(t *testing.T, values map[string]string) (deploymentConfig, error) {
	t.Helper()
	values["LyricsPipeline-Separation:baseStack"] = "org/LyricsPipeline/dev"
	var got deploymentConfig
	var loadErr error
	err := pulumi.RunErr(func(ctx *pulumi.Context) error { got, loadErr = loadConfig(ctx); return nil }, pulumi.WithMocks("LyricsPipeline-Separation", "test", &infratest.Mocks{}), func(info *pulumi.RunInfo) { info.Config = values })
	if err != nil {
		t.Fatal(err)
	}
	return got, loadErr
}
func TestNativeConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, key, value, field string }{
		{name: "central values"},
		{"zero minimum", "autoscaling", `{"minTasks":0}`, ""},
		{"positive minimum", "autoscaling", `{"minTasks":1}`, ""},
		{"negative minimum", "autoscaling", `{"minTasks":-1}`, "minTasks"},
		{"zero maximum", "autoscaling", `{"maxTasks":0}`, "maxTasks"},
		{"negative maximum", "autoscaling", `{"maxTasks":-1}`, "maxTasks"},
		{"maximum below minimum", "autoscaling", `{"minTasks":3,"maxTasks":2}`, "maxTasks"},
		{"no idle period", "autoscaling", `{"idleMinutes":0}`, "idleMinutes"},
		{"negative idle period", "autoscaling", `{"idleMinutes":-1}`, "idleMinutes"},
		{"one idle minute", "autoscaling", `{"idleMinutes":1}`, ""},
		{"excess idle window", "autoscaling", `{"idleMinutes":1441}`, "idleMinutes"},
		{"custom scaling", "autoscaling", `{"backlogPerWorker":3,"scaleOutCooldownSeconds":600}`, ""},
		{"zero backlog", "autoscaling", `{"backlogPerWorker":0}`, "backlogPerWorker"},
		{"negative backlog", "autoscaling", `{"backlogPerWorker":-1}`, "backlogPerWorker"},
		{"fractional backlog", "autoscaling", `{"backlogPerWorker":2.5}`, "backlogPerWorker"},
		{"zero cooldown", "autoscaling", `{"scaleOutCooldownSeconds":0}`, "scaleOutCooldownSeconds"},
		{"negative cooldown", "autoscaling", `{"scaleOutCooldownSeconds":-1}`, "scaleOutCooldownSeconds"},
		{"overflow cooldown", "autoscaling", `{"scaleOutCooldownSeconds":9223372036854775807}`, "scaleOutCooldownSeconds"},
		{"malformed object", "ecs", `{`, "ecs"},
		{"null object", "ecs", `null`, "ecs"},
		{"null tags", "tags", `null`, "tags"},
		{"null CPU", "ecs", `{"cpuUnits":null}`, "cpuUnits"},
		{"null count", "autoscaling", `{"maxTasks":null}`, "maxTasks"},
		{"array", "ecs", `[]`, "ecs"},
		{"trailing object", "ecs", `{} {}`, "ecs"},
		{"trailing junk", "ecs", `{} nope`, "ecs"},
		{"unknown field", "autoscaling", `{"maxTask":2}`, "maxTask"},
		{"empty name", "name", " ", "name"},
		{"empty zone", "vpc", `{"availabilityZone":""}`, "vpc.availabilityZone"},
		{"CPU must be numeric", "ecs", `{"cpuUnits":"2048"}`, "cpuUnits"},
		{"memory must be numeric", "ecs", `{"memoryMiB":"8192"}`, "memoryMiB"},
		{"incompatible memory", "ecs", `{"memoryMiB":2048}`, "ecs.cpuUnits/memoryMiB"},
		{"unsupported CPU", "ecs", `{"cpuUnits":123}`, "ecs.cpuUnits/memoryMiB"},
		{"legacy deployment", "deployment", `{}`, "legacy configuration"},
		{"legacy image", "deploymentImage", "registry/image:tag", "legacy configuration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := nativeValues(t)
			if tc.key != "" {
				key := "LyricsPipeline-Separation:" + tc.key
				if tc.key == "name" || tc.key == "image" || tc.key == "deployment" || tc.key == "deploymentImage" || tc.key == "tags" {
					values[key] = tc.value
				} else {
					configtest.EditObject(t, values, key, tc.value)
				}
			}
			_, err := loadForTest(t, values)
			if tc.field == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("got %v, expected error for %s", err, tc.field)
			}
		})
	}
}
func TestNativeStackFile(t *testing.T) {
	if _, err := loadForTest(t, configtest.StackValuesAt(t, "Pulumi.yaml", "dev")); err != nil {
		t.Fatal(err)
	}
}
func TestMissingConfigurationHasNoFallback(t *testing.T) {
	for _, key := range []string{"name", "image", "tags", "vpc", "ecs", "autoscaling"} {
		t.Run(key, func(t *testing.T) {
			values := nativeValues(t)
			delete(values, "LyricsPipeline-Separation:"+key)
			if _, err := loadForTest(t, values); err == nil {
				t.Fatal("missing configuration accepted")
			}
		})
	}
	for _, key := range []string{"vpc", "ecs", "autoscaling"} {
		t.Run("partial_"+key, func(t *testing.T) {
			values := nativeValues(t)
			values["LyricsPipeline-Separation:"+key] = `{}`
			if _, err := loadForTest(t, values); err == nil || !strings.Contains(err.Error(), "missing required field") {
				t.Fatalf("partial override accepted: %v", err)
			}
		})
	}
	values := nativeValues(t)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(values["LyricsPipeline-Separation:autoscaling"]), &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "minTasks")
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	values["LyricsPipeline-Separation:autoscaling"] = string(raw)
	if _, err := loadForTest(t, values); err == nil || !strings.Contains(err.Error(), "minTasks") {
		t.Fatalf("missing zero-valued setting accepted: %v", err)
	}
}
func TestOperationalLimits(t *testing.T) {
	for _, tc := range []struct{ key, patch, field string }{
		{"ecs", `{"stopTimeoutSeconds":121}`, "stopTimeoutSeconds"},
		{"ecs", `{"logRetentionDays":9223372036854775807}`, "logRetentionDays"},
		{"autoscaling", `{"scaleInCooldownSeconds":0}`, "scaleInCooldownSeconds"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			values := nativeValues(t)
			configtest.EditObject(t, values, "LyricsPipeline-Separation:"+tc.key, tc.patch)
			_, err := loadForTest(t, values)
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("expected %s error, got %v", tc.field, err)
			}
		})
	}
}
func TestCentralEditsAreAccepted(t *testing.T) {
	values := nativeValues(t)
	configtest.EditObject(t, values, "LyricsPipeline-Separation:autoscaling", `{"maxTasks":4}`)
	got, err := loadForTest(t, values)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scaling.MaxTasks != 4 {
		t.Fatalf("central setting ignored: %d", got.Scaling.MaxTasks)
	}
}
func TestLoadsAreIndependent(t *testing.T) {
	first, err := loadForTest(t, nativeValues(t))
	if err != nil {
		t.Fatal(err)
	}
	original, err := loadForTest(t, nativeValues(t))
	if err != nil {
		t.Fatal(err)
	}
	first.Tags["Purpose"] = "changed"
	first.Task.HealthCheck.Command[0] = "changed"
	second, err := loadForTest(t, nativeValues(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, second) {
		t.Fatal("configuration loads share mutable state")
	}
}
func TestNetworkValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*vpc.Settings)
	}{
		{"invalid", func(n *vpc.Settings) { n.VPC = netip.Prefix{} }},
		{"host bits", func(n *vpc.Settings) { n.PublicSubnet = netip.MustParsePrefix("10.42.1.5/24") }},
		{"outside VPC", func(n *vpc.Settings) { n.PublicSubnet = netip.MustParsePrefix("10.43.1.0/24") }},
		{"overlap", func(n *vpc.Settings) { n.PrivateSubnet = n.PublicSubnet }},
		{"IPv6", func(n *vpc.Settings) { n.Internet = netip.MustParsePrefix("::/0") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, err := loadForTest(t, nativeValues(t))
			if err != nil {
				t.Fatal(err)
			}
			n := settings.Network
			tc.change(&n)
			if err := n.Validate(); err == nil {
				t.Fatal("invalid network accepted")
			}
		})
	}
}
func TestNativeSettingsAndUnits(t *testing.T) {
	values := nativeValues(t)
	configtest.EditObject(t, values, "LyricsPipeline-Separation:ecs", `{"cpuUnits":1024,"memoryMiB":4096}`)
	values["LyricsPipeline-Separation:image"] = "registry/image@sha256:digest"
	configtest.EditObject(t, values, "LyricsPipeline-Separation:autoscaling", `{"backlogPerWorker":3,"scaleOutCooldownSeconds":600,"idleMinutes":7}`)
	values["LyricsPipeline-Separation:tags"] = `{"Owner":"team"}`
	got, err := loadForTest(t, values)
	if err != nil {
		t.Fatal(err)
	}
	if got.Image != "registry/image@sha256:digest" || got.Task.Resources.CPU != 1024 || got.Task.Resources.Memory != 4096 || got.Scaling.BacklogPerWorker != 3 || got.Scaling.ScaleOutCooldown != 10*time.Minute || got.Scaling.IdleWindow != 7*time.Minute {
		t.Fatalf("incorrect native settings conversion: %+v", got)
	}
	if len(got.Tags) != 1 || got.Tags["Owner"] != "team" {
		t.Fatalf("unexpected tags: %v", got.Tags)
	}
}
func TestCentralOperationalSettings(t *testing.T) {
	values := nativeValues(t)
	configtest.EditObject(t, values, "LyricsPipeline-Separation:vpc", `{"availabilityZone":"eu-central-1b","cidr":"10.50.0.0/16","publicSubnetCIDR":"10.50.1.0/24","privateSubnetCIDR":"10.50.2.0/24"}`)
	configtest.EditObject(t, values, "LyricsPipeline-Separation:ecs", `{"stopTimeoutSeconds":60,"logRetentionDays":7,"healthCheck":{"command":["CMD","python"],"intervalSeconds":30,"timeoutSeconds":10,"retries":5,"startPeriodSeconds":0}}`)
	configtest.EditObject(t, values, "LyricsPipeline-Separation:autoscaling", `{"scaleInCooldownSeconds":60}`)
	got, err := loadForTest(t, values)
	if err != nil {
		t.Fatal(err)
	}
	if got.AvailabilityZone != "eu-central-1b" || got.Network.VPC.String() != "10.50.0.0/16" || got.Network.PublicSubnet.String() != "10.50.1.0/24" {
		t.Fatalf("network configuration ignored: %+v", got.Network)
	}
	if got.Task.StopTimeout != time.Minute || got.Task.LogRetention != 7*24*time.Hour || got.Task.HealthCheck.Interval != 30*time.Second || got.Task.HealthCheck.StartPeriod != 0 || got.Task.HealthCheck.Retries != 5 {
		t.Fatalf("task configuration ignored: %+v", got.Task)
	}
	if got.Scaling.ScaleInCooldown != time.Minute {
		t.Fatal("operational configuration ignored")
	}
}
