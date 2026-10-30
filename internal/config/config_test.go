package config

import (
	"net/netip"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct{ name, raw, field string }{
		{"defaults", "", ""}, {"zero minimum", `{"minTasks":0}`, ""}, {"positive minimum", `{"minTasks":1}`, ""},
		{"negative minimum", `{"minTasks":-1}`, "minTasks"}, {"zero maximum", `{"maxTasks":0}`, "maxTasks"},
		{"negative maximum", `{"maxTasks":-1}`, "maxTasks"}, {"maximum below minimum", `{"minTasks":3,"maxTasks":2}`, "maxTasks"},
		{"one receive", `{"maxReceiveCount":1}`, "maxReceiveCount"}, {"zero receives", `{"maxReceiveCount":0}`, "maxReceiveCount"},
		{"two receives", `{"maxReceiveCount":2}`, ""}, {"excess receives", `{"maxReceiveCount":1001}`, "maxReceiveCount"},
		{"no idle period", `{"idleMinutes":0}`, "idleMinutes"}, {"negative idle period", `{"idleMinutes":-1}`, "idleMinutes"},
		{"one idle minute", `{"idleMinutes":1}`, ""}, {"excess idle window", `{"idleMinutes":1441}`, "idleMinutes"},
		{"custom scaling", `{"backlogPerWorker":3,"scaleOutCooldownSeconds":600}`, ""},
		{"zero backlog", `{"backlogPerWorker":0}`, "backlogPerWorker"}, {"negative backlog", `{"backlogPerWorker":-1}`, "backlogPerWorker"},
		{"fractional backlog", `{"backlogPerWorker":2.5}`, "backlogPerWorker"},
		{"zero cooldown", `{"scaleOutCooldownSeconds":0}`, "scaleOutCooldownSeconds"}, {"negative cooldown", `{"scaleOutCooldownSeconds":-1}`, "scaleOutCooldownSeconds"},
		{"overflow cooldown", `{"scaleOutCooldownSeconds":9223372036854775807}`, "scaleOutCooldownSeconds"},
		{"malformed JSON", `{`, "deployment"}, {"null", `null`, "deployment"}, {"array", `[]`, "deployment"},
		{"trailing object", `{} {}`, "deployment"}, {"trailing junk", `{} nope`, "deployment"}, {"unknown field", `{"maxTask":2}`, "maxTask"},
		{"empty name", `{"name":" "}`, "name"}, {"empty region", `{"region":""}`, "region"}, {"empty zone", `{"availabilityZone":""}`, "availabilityZone"},
		{"bad cpu", `{"taskCpu":"two"}`, "taskCpu"}, {"bad memory", `{"taskMemory":"8 GB"}`, "taskMemory"},
		{"numeric cpu", `{"taskCpu":2048}`, "taskCpu"}, {"incompatible memory", `{"taskMemory":"2048"}`, "taskCpu/taskMemory"},
		{"unsupported cpu", `{"taskCpu":"123"}`, "taskCpu/taskMemory"},
		{"zero visibility", `{"visibilitySeconds":0}`, "visibilitySeconds"}, {"excess visibility", `{"visibilitySeconds":43201}`, "visibilitySeconds"},
		{"visibility boundary", `{"visibilitySeconds":43200}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.raw, "")
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

func TestConfigurationCompatibility(t *testing.T) {
	data, err := os.ReadFile("../../deployment.example.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(string(data), "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, Defaults()) {
		t.Fatalf("example differs from defaults: %+v", got)
	}
	got, err = Parse(`{"image":"configured:tag","backlogPerWorker":3,"scaleOutCooldownSeconds":600,"visibilitySeconds":123,"idleMinutes":7,"tags":{"Owner":"team"}}`, "registry/image@sha256:digest")
	if err != nil {
		t.Fatal(err)
	}
	if got.Image != "registry/image@sha256:digest" || got.Scaling.BacklogPerWorker != 3 || got.Scaling.ScaleOutCooldown != 10*time.Minute || got.Scaling.IdleWindow != 7*time.Minute || got.Queue.VisibilityTimeout != 123*time.Second {
		t.Fatalf("incorrect conversion or precedence: %+v", got)
	}
	if got.Tags["Purpose"] != "vocal-extractor" || got.Tags["Owner"] != "team" {
		t.Fatalf("tag merge changed: %v", got.Tags)
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
			n := Defaults().Network
			tc.change(&n)
			if err := n.Validate(); err == nil {
				t.Fatal("invalid network accepted")
			}
		})
	}
}

func TestDefaultsAreIndependent(t *testing.T) {
	first := Defaults()
	first.Tags["Purpose"] = "changed"
	first.Task.HealthCheck.Command[0] = "changed"
	second := Defaults()
	if second.Tags["Purpose"] != "vocal-extractor" || second.Task.HealthCheck.Command[0] != "CMD-SHELL" {
		t.Fatal("defaults share mutable state")
	}
}
