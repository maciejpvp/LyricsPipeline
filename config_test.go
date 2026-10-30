package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestInfrastructureConfig(t *testing.T) {
	tests := []struct {
		name, raw string
		invalid   bool
	}{
		{"defaults", "", false},
		{"zero minimum", `{"minTasks":0}`, false},
		{"positive minimum", `{"minTasks":1}`, false},
		{"negative minimum", `{"minTasks":-1}`, true},
		{"zero maximum", `{"maxTasks":0}`, true},
		{"negative maximum", `{"maxTasks":-1}`, true},
		{"maximum below minimum", `{"minTasks":3,"maxTasks":2}`, true},
		{"one receive", `{"maxReceiveCount":1}`, true},
		{"zero receives", `{"maxReceiveCount":0}`, true},
		{"two receives", `{"maxReceiveCount":2}`, false},
		{"no idle period", `{"idleMinutes":0}`, true},
		{"negative idle period", `{"idleMinutes":-1}`, true},
		{"one idle minute", `{"idleMinutes":1}`, false},
		{"custom backlog and cooldown", `{"backlogPerWorker":3,"scaleOutCooldownSeconds":600}`, false},
		{"zero backlog", `{"backlogPerWorker":0}`, true},
		{"negative backlog", `{"backlogPerWorker":-1}`, true},
		{"fractional backlog", `{"backlogPerWorker":2.5}`, true},
		{"zero cooldown", `{"scaleOutCooldownSeconds":0}`, true},
		{"negative cooldown", `{"scaleOutCooldownSeconds":-1}`, true},
		{"malformed JSON", `{`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pulumi.RunErr(func(ctx *pulumi.Context) error {
				cfg, err := loadInfrastructureConfig(ctx)
				if (err != nil) != tt.invalid {
					t.Fatalf("config error = %v, want invalid %v", err, tt.invalid)
				}
				if tt.raw == "" && (cfg.MinTasks != 0 || cfg.MaxTasks != 2 || cfg.IdleMinutes != 5 || cfg.MaxReceiveCount != 5 || cfg.TaskCPU != "2048" || cfg.TaskMemory != "8192" || cfg.BacklogPerWorker != 5 || cfg.ScaleOutCooldownSeconds != 300) {
					t.Fatalf("unexpected defaults: %+v", cfg)
				}
				if tt.raw == `{"backlogPerWorker":3,"scaleOutCooldownSeconds":600}` && (cfg.BacklogPerWorker != 3 || cfg.ScaleOutCooldownSeconds != 600) {
					t.Fatalf("custom scaling settings were not loaded: %+v", cfg)
				}
				return nil
			}, pulumi.WithMocks("LyricsPipeline", "test", &infraMocks{}), withDeployment(tt.raw))
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExampleMatchesDefaults(t *testing.T) {
	data, err := os.ReadFile("deployment.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var cfg InfrastructureConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.MinTasks != 0 || cfg.MaxTasks != 2 || cfg.MaxReceiveCount != 5 || cfg.IdleMinutes != 5 || cfg.TaskCPU != "2048" || cfg.TaskMemory != "8192" || cfg.BacklogPerWorker != 5 || cfg.ScaleOutCooldownSeconds != 300 {
		t.Fatalf("unexpected example settings: %+v", cfg)
	}
}

func withDeployment(raw string) pulumi.RunOption {
	return func(info *pulumi.RunInfo) {
		info.Config = map[string]string{"LyricsPipeline:deployment": raw}
	}
}
