package main

import (
	"encoding/json"
	"fmt"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

type InfrastructureConfig struct {
	Name                    string            `json:"name"`
	Region                  string            `json:"region"`
	AvailabilityZone        string            `json:"availabilityZone"`
	Image                   string            `json:"image"`
	TaskCPU                 string            `json:"taskCpu"`
	TaskMemory              string            `json:"taskMemory"`
	MinTasks                int               `json:"minTasks"`
	MaxTasks                int               `json:"maxTasks"`
	Visibility              int               `json:"visibilitySeconds"`
	MaxReceiveCount         int               `json:"maxReceiveCount"`
	IdleMinutes             int               `json:"idleMinutes"`
	BacklogPerWorker        int               `json:"backlogPerWorker"`
	ScaleOutCooldownSeconds int               `json:"scaleOutCooldownSeconds"`
	Tags                    map[string]string `json:"tags"`
}

func defaultInfrastructureConfig() InfrastructureConfig {
	return InfrastructureConfig{
		Name:                    "vocal-extractor",
		Region:                  "eu-central-1",
		AvailabilityZone:        "eu-central-1a",
		Image:                   "",
		TaskCPU:                 "2048",
		TaskMemory:              "8192",
		MinTasks:                0,
		MaxTasks:                2,
		Visibility:              900,
		MaxReceiveCount:         5,
		IdleMinutes:             5,
		BacklogPerWorker:        5,
		ScaleOutCooldownSeconds: 300,
		Tags:                    map[string]string{"Purpose": "vocal-extractor"},
	}
}

func loadInfrastructureConfig(ctx *pulumi.Context) (InfrastructureConfig, error) {
	config := defaultInfrastructureConfig()
	if raw, ok := ctx.GetConfig(ctx.Project() + ":deployment"); ok && raw != "" {
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return InfrastructureConfig{}, fmt.Errorf("invalid deployment configuration: %w", err)
		}
	}
	if image, ok := ctx.GetConfig(ctx.Project() + ":deploymentImage"); ok && image != "" {
		config.Image = image
	}
	if config.Name == "" || config.Region == "" || config.AvailabilityZone == "" {
		return InfrastructureConfig{}, fmt.Errorf("deployment configuration requires name, region, and availabilityZone")
	}
	if config.TaskCPU == "" || config.TaskMemory == "" || config.MinTasks < 0 || config.MaxTasks < 1 || config.MaxTasks < config.MinTasks || config.Visibility < 1 || config.MaxReceiveCount < 2 || config.IdleMinutes < 1 {
		return InfrastructureConfig{}, fmt.Errorf("deployment configuration contains invalid compute or queue limits")
	}
	if config.BacklogPerWorker < 1 || config.ScaleOutCooldownSeconds < 1 {
		return InfrastructureConfig{}, fmt.Errorf("deployment configuration requires positive backlogPerWorker and scaleOutCooldownSeconds")
	}
	return config, nil
}

func (config InfrastructureConfig) pulumiTags() pulumi.StringMap {
	tags := pulumi.StringMap{}
	for key, value := range config.Tags {
		tags[key] = pulumi.String(value)
	}
	return tags
}
