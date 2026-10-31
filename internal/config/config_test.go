package config

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"

	"LyricsPipeline/internal/configtest"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type configMocks struct{}

func (configMocks) Call(args pulumi.MockCallArgs) (resource.PropertyMap, error) {
	return nil, fmt.Errorf("unexpected invoke: %s", args.Token)
}
func (configMocks) NewResource(args pulumi.MockResourceArgs) (string, resource.PropertyMap, error) {
	return "", nil, fmt.Errorf("unexpected resource: %s", args.Name)
}

func nativeValues(t *testing.T) map[string]string { return configtest.ProjectValues(t) }

func loadConfig(t *testing.T, values map[string]string) (Deployment, error) {
	t.Helper()
	var got Deployment
	var loadErr error
	err := pulumi.RunErr(func(ctx *pulumi.Context) error { got, loadErr = Load(ctx); return nil },
		pulumi.WithMocks("LyricsPipeline", "test", configMocks{}), func(info *pulumi.RunInfo) { info.Config = values })
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
		{"one receive", "sqs", `{"maxReceiveCount":1}`, "maxReceiveCount"},
		{"zero receives", "sqs", `{"maxReceiveCount":0}`, "maxReceiveCount"},
		{"two receives", "sqs", `{"maxReceiveCount":2}`, ""},
		{"excess receives", "sqs", `{"maxReceiveCount":1001}`, "maxReceiveCount"},
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
		{"zero visibility", "sqs", `{"visibilitySeconds":0}`, "visibilitySeconds"},
		{"excess visibility", "sqs", `{"visibilitySeconds":43201}`, "visibilitySeconds"},
		{"visibility boundary", "sqs", `{"visibilitySeconds":43200}`, ""},
		{"legacy deployment", "deployment", `{}`, "legacy configuration"},
		{"legacy image", "deploymentImage", "registry/image:tag", "legacy configuration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := nativeValues(t)
			if tc.key != "" {
				key := "LyricsPipeline:" + tc.key
				if tc.key == "name" || tc.key == "image" || tc.key == "deployment" || tc.key == "deploymentImage" || tc.key == "tags" {
					values[key] = tc.value
				} else {
					configtest.EditObject(t, values, key, tc.value)
				}
			}
			_, err := loadConfig(t, values)
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

func TestLocationMustBeExplicit(t *testing.T) {
	for _, key := range []string{"aws:region", "LyricsPipeline:vpc"} {
		t.Run(key, func(t *testing.T) {
			values := nativeValues(t)
			delete(values, key)
			if _, err := loadConfig(t, values); err == nil {
				t.Fatal("missing location accepted")
			}
		})
	}
}

func TestNativeStackFile(t *testing.T) {
	if _, err := loadConfig(t, configtest.StackValues(t, "dev")); err != nil {
		t.Fatal(err)
	}
}

func TestCentralEditsAreAccepted(t *testing.T) {
	values := nativeValues(t)
	configtest.EditObject(t, values, "LyricsPipeline:autoscaling", `{"maxTasks":4}`)
	got, err := loadConfig(t, values)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scaling.MaxTasks != 4 {
		t.Fatalf("central setting ignored: %d", got.Scaling.MaxTasks)
	}
}

func TestMissingConfigurationHasNoFallback(t *testing.T) {
	for _, key := range []string{"name", "image", "tags", "vpc", "s3", "ecs", "sqs", "autoscaling"} {
		t.Run(key, func(t *testing.T) {
			values := nativeValues(t)
			delete(values, "LyricsPipeline:"+key)
			if _, err := loadConfig(t, values); err == nil {
				t.Fatal("missing configuration accepted")
			}
		})
	}
	for _, key := range []string{"vpc", "s3", "ecs", "sqs", "autoscaling"} {
		t.Run("partial_"+key, func(t *testing.T) {
			values := nativeValues(t)
			values["LyricsPipeline:"+key] = `{}`
			if _, err := loadConfig(t, values); err == nil || !strings.Contains(err.Error(), "missing required field") {
				t.Fatalf("partial override accepted: %v", err)
			}
		})
	}
	values := nativeValues(t)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(values["LyricsPipeline:autoscaling"]), &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "minTasks")
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	values["LyricsPipeline:autoscaling"] = string(raw)
	if _, err := loadConfig(t, values); err == nil || !strings.Contains(err.Error(), "minTasks") {
		t.Fatalf("missing zero-valued setting accepted: %v", err)
	}
}

func TestNativeSettingsAndUnits(t *testing.T) {
	values := nativeValues(t)
	configtest.EditObject(t, values, "LyricsPipeline:ecs", `{"cpuUnits":1024,"memoryMiB":4096}`)
	values["LyricsPipeline:image"] = "registry/image@sha256:digest"
	configtest.EditObject(t, values, "LyricsPipeline:autoscaling", `{"backlogPerWorker":3,"scaleOutCooldownSeconds":600,"idleMinutes":7}`)
	configtest.EditObject(t, values, "LyricsPipeline:sqs", `{"visibilitySeconds":123}`)
	values["LyricsPipeline:tags"] = `{"Owner":"team"}`
	got, err := loadConfig(t, values)
	if err != nil {
		t.Fatal(err)
	}
	if got.Image != "registry/image@sha256:digest" || got.Task.Resources.CPU != 1024 || got.Task.Resources.Memory != 4096 || got.Scaling.BacklogPerWorker != 3 || got.Scaling.ScaleOutCooldown != 10*time.Minute || got.Scaling.IdleWindow != 7*time.Minute || got.Queue.VisibilityTimeout != 123*time.Second {
		t.Fatalf("incorrect native settings conversion: %+v", got)
	}
	if len(got.Tags) != 1 || got.Tags["Owner"] != "team" {
		t.Fatalf("unexpected tags: %v", got.Tags)
	}
}

func TestFargateResources(t *testing.T) {
	for _, tc := range []struct {
		cpu    CPUUnits
		memory MemoryMiB
		valid  bool
	}{
		{256, 512, true}, {256, 1024, true}, {256, 2048, true}, {256, 1536, false},
		{512, 1024, true}, {512, 4096, true}, {512, 512, false}, {512, 5120, false},
		{1024, 2048, true}, {1024, 8192, true}, {1024, 9216, false},
		{2048, 4096, true}, {2048, 8192, true}, {2048, 16384, true}, {2048, 4097, false},
		{4096, 8192, true}, {4096, 30720, true}, {4096, 31744, false},
		{8192, 16384, true}, {8192, 61440, true}, {8192, 17408, false},
		{16384, 32768, true}, {16384, 122880, true}, {16384, 36864, false},
		{32768, 61440, true}, {32768, 122880, true}, {32768, 249856, true}, {32768, 65536, false},
		{0, 8192, false}, {-1, 8192, false}, {2048, -1, false},
	} {
		if err := (TaskResources{CPU: tc.cpu, Memory: tc.memory}).Validate(); (err == nil) != tc.valid {
			t.Errorf("CPU=%d memory=%d: %v, valid=%v", tc.cpu, tc.memory, err, tc.valid)
		}
	}
}

func TestNetworkValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Network)
	}{
		{"invalid", func(n *Network) { n.VPC = netip.Prefix{} }},
		{"host bits", func(n *Network) { n.PublicSubnet = netip.MustParsePrefix("10.42.1.5/24") }},
		{"outside VPC", func(n *Network) { n.PublicSubnet = netip.MustParsePrefix("10.43.1.0/24") }},
		{"overlap", func(n *Network) { n.PrivateSubnet = n.PublicSubnet }},
		{"IPv6", func(n *Network) { n.Internet = netip.MustParsePrefix("::/0") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings, err := loadConfig(t, nativeValues(t))
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

func TestLoadsAreIndependent(t *testing.T) {
	first, err := loadConfig(t, nativeValues(t))
	if err != nil {
		t.Fatal(err)
	}
	original, err := loadConfig(t, nativeValues(t))
	if err != nil {
		t.Fatal(err)
	}
	first.Tags["Purpose"] = "changed"
	first.Task.HealthCheck.Command[0] = "changed"
	second, err := loadConfig(t, nativeValues(t))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(original, second) {
		t.Fatal("configuration loads share mutable state")
	}
}

func TestCentralOperationalSettings(t *testing.T) {
	values := nativeValues(t)
	configtest.EditObject(t, values, "LyricsPipeline:vpc", `{"availabilityZone":"eu-central-1b","cidr":"10.50.0.0/16","publicSubnetCIDR":"10.50.1.0/24","privateSubnetCIDR":"10.50.2.0/24"}`)
	configtest.EditObject(t, values, "LyricsPipeline:s3", `{"inputPrefix":"uploads/audio","outputPrefix":"results/stems"}`)
	configtest.EditObject(t, values, "LyricsPipeline:ecs", `{"stopTimeoutSeconds":60,"logRetentionDays":7,"healthCheck":{"command":["CMD","python"],"intervalSeconds":30,"timeoutSeconds":10,"retries":5,"startPeriodSeconds":0}}`)
	configtest.EditObject(t, values, "LyricsPipeline:sqs", `{"longPollWaitSeconds":0,"deadLetterRetentionDays":7}`)
	configtest.EditObject(t, values, "LyricsPipeline:autoscaling", `{"scaleInCooldownSeconds":60}`)
	got, err := loadConfig(t, values)
	if err != nil {
		t.Fatal(err)
	}
	if got.AvailabilityZone != "eu-central-1b" || got.Network.VPC.String() != "10.50.0.0/16" || got.Network.PublicSubnet.String() != "10.50.1.0/24" {
		t.Fatalf("network configuration ignored: %+v", got.Network)
	}
	if got.Objects.Input != "uploads/audio" || got.Objects.Output != "results/stems" {
		t.Fatalf("object configuration ignored: %+v", got.Objects)
	}
	if got.Task.StopTimeout != time.Minute || got.Task.LogRetention != 7*24*time.Hour || got.Task.HealthCheck.Interval != 30*time.Second || got.Task.HealthCheck.StartPeriod != 0 || got.Task.HealthCheck.Retries != 5 {
		t.Fatalf("task configuration ignored: %+v", got.Task)
	}
	if got.Queue.LongPollWait != 0 || got.Queue.DeadLetterRetention != 7*24*time.Hour || got.Scaling.ScaleInCooldown != time.Minute {
		t.Fatal("operational configuration ignored")
	}
}

func TestOperationalLimits(t *testing.T) {
	for _, tc := range []struct{ key, patch, field string }{
		{"ecs", `{"stopTimeoutSeconds":121}`, "stopTimeoutSeconds"},
		{"ecs", `{"logRetentionDays":9223372036854775807}`, "logRetentionDays"},
		{"sqs", `{"longPollWaitSeconds":21}`, "longPollWaitSeconds"},
		{"sqs", `{"deadLetterRetentionDays":15}`, "deadLetterRetentionDays"},
		{"autoscaling", `{"scaleInCooldownSeconds":0}`, "scaleInCooldownSeconds"},
		{"s3", `{"inputPrefix":""}`, "inputPrefix"},
		{"s3", `{"outputPrefix":"input/results"}`, "outputPrefix"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			values := nativeValues(t)
			configtest.EditObject(t, values, "LyricsPipeline:"+tc.key, tc.patch)
			_, err := loadConfig(t, values)
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("expected %s error, got %v", tc.field, err)
			}
		})
	}
}
