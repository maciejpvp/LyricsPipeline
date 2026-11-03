package main

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strings"
	"time"

	"LyricsPipeline/internal/autoscaling"
	"LyricsPipeline/internal/config"
	"LyricsPipeline/internal/ecs"
	"LyricsPipeline/internal/vpc"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	pc "github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

type deploymentConfig struct {
	Name, Region                       string
	Tags                               map[string]string
	BaseStack, AvailabilityZone, Image string
	Network                            vpc.Settings
	Task                               Task
	Scaling                            autoscaling.Settings
}
type Task struct {
	Resources    ecs.Resources
	StopTimeout  time.Duration
	LogRetention time.Duration
	HealthCheck  HealthCheck
}

type HealthCheck struct {
	Command     []string
	Interval    time.Duration
	Timeout     time.Duration
	Retries     int
	StartPeriod time.Duration
}

type vpcSettings struct {
	AvailabilityZone  string `json:"availabilityZone"`
	CIDR              string `json:"cidr"`
	PrivateSubnetCIDR string `json:"privateSubnetCIDR"`
}
type ecsSettings struct {
	CPUUnits           ecs.CPUUnits        `json:"cpuUnits"`
	MemoryMiB          ecs.MemoryMiB       `json:"memoryMiB"`
	StopTimeoutSeconds int                 `json:"stopTimeoutSeconds"`
	LogRetentionDays   int                 `json:"logRetentionDays"`
	HealthCheck        healthCheckSettings `json:"healthCheck"`
}
type healthCheckSettings struct {
	Command            []string `json:"command"`
	IntervalSeconds    int      `json:"intervalSeconds"`
	TimeoutSeconds     int      `json:"timeoutSeconds"`
	Retries            int      `json:"retries"`
	StartPeriodSeconds int      `json:"startPeriodSeconds"`
}
type autoscalingSettings struct {
	MinTasks                int `json:"minTasks"`
	MaxTasks                int `json:"maxTasks"`
	IdleMinutes             int `json:"idleMinutes"`
	BacklogPerWorker        int `json:"backlogPerWorker"`
	ScaleOutCooldownSeconds int `json:"scaleOutCooldownSeconds"`
	ScaleInCooldownSeconds  int `json:"scaleInCooldownSeconds"`
}

func loadMetadata(ctx *pulumi.Context) (deploymentConfig, error) {
	c := pc.New(ctx, "")
	var d deploymentConfig
	var err error
	for _, key := range []string{"deployment", "deploymentImage"} {
		if _, e := c.Try(key); e == nil {
			return d, fmt.Errorf("legacy configuration %s: migrate configuration before splitting stacks", key)
		}
	}
	if d.Name, err = c.Try("name"); err != nil {
		return d, err
	}
	if d.Region, err = pc.New(ctx, "aws").Try("region"); err != nil {
		return d, err
	}
	if strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Region) == "" {
		return d, errors.New("name and aws:region must not be empty")
	}
	if err = c.TryObject("tags", &d.Tags); err != nil {
		return d, err
	}
	if d.Tags == nil {
		return d, errors.New("tags must be an object")
	}
	return d, nil
}

func (s *vpcSettings) UnmarshalJSON(data []byte) error {
	type fields vpcSettings
	return config.DecodeObject(data, (*fields)(s))
}
func (s *ecsSettings) UnmarshalJSON(data []byte) error {
	type fields ecsSettings
	return config.DecodeObject(data, (*fields)(s))
}
func (s *healthCheckSettings) UnmarshalJSON(data []byte) error {
	type fields healthCheckSettings
	return config.DecodeObject(data, (*fields)(s))
}
func (s *autoscalingSettings) UnmarshalJSON(data []byte) error {
	type fields autoscalingSettings
	return config.DecodeObject(data, (*fields)(s))
}
func (s vpcSettings) apply(d *deploymentConfig) error {
	d.AvailabilityZone = s.AvailabilityZone
	for _, field := range []struct {
		name, value string
		target      *netip.Prefix
	}{
		{"cidr", s.CIDR, &d.Network.VPC}, {"privateSubnetCIDR", s.PrivateSubnetCIDR, &d.Network.PrivateSubnet},
	} {
		prefix, err := netip.ParsePrefix(field.value)
		if err != nil {
			return fmt.Errorf("vpc.%s: %w", field.name, err)
		}
		*field.target = prefix
	}
	return nil
}
func (s ecsSettings) apply(task *Task) error {
	const maximumStopTimeout = 2 * time.Minute
	if s.StopTimeoutSeconds < 2 || s.StopTimeoutSeconds > int(maximumStopTimeout/time.Second) {
		return errors.New("ecs.stopTimeoutSeconds: must be between 2 and 120")
	}
	// Bound conversion to time.Duration; CloudWatch validates its supported day choices.
	if s.LogRetentionDays < 1 || s.LogRetentionDays > int(time.Duration(math.MaxInt64)/(24*time.Hour)) {
		return errors.New("ecs.logRetentionDays: must be positive and fit a duration")
	}
	health, err := s.HealthCheck.domain()
	if err != nil {
		return err
	}
	task.Resources = ecs.Resources{CPU: s.CPUUnits, Memory: s.MemoryMiB}
	task.StopTimeout = time.Duration(s.StopTimeoutSeconds) * time.Second
	task.LogRetention = time.Duration(s.LogRetentionDays) * 24 * time.Hour
	task.HealthCheck = health
	return nil
}
func (s autoscalingSettings) apply(scaling *autoscaling.Settings) error {
	const maximumIdleWindow = 24 * time.Hour
	if s.IdleMinutes < 1 || s.IdleMinutes > int(maximumIdleWindow/time.Minute) {
		return errors.New("autoscaling.idleMinutes: must be between 1 and 1440")
	}
	if s.ScaleOutCooldownSeconds < 1 || s.ScaleOutCooldownSeconds > math.MaxInt32 {
		return errors.New("autoscaling.scaleOutCooldownSeconds: must be a positive AWS 32-bit integer")
	}
	if s.ScaleInCooldownSeconds < 1 || s.ScaleInCooldownSeconds > math.MaxInt32 {
		return errors.New("autoscaling.scaleInCooldownSeconds: must be a positive AWS 32-bit integer")
	}
	scaling.MinTasks = s.MinTasks
	scaling.MaxTasks = s.MaxTasks
	scaling.IdleWindow = time.Duration(s.IdleMinutes) * time.Minute
	scaling.BacklogPerWorker = s.BacklogPerWorker
	scaling.ScaleOutCooldown = time.Duration(s.ScaleOutCooldownSeconds) * time.Second
	scaling.ScaleInCooldown = time.Duration(s.ScaleInCooldownSeconds) * time.Second
	return nil
}
func (h healthCheckSettings) domain() (HealthCheck, error) {
	if len(h.Command) < 2 || (h.Command[0] != "CMD" && h.Command[0] != "CMD-SHELL") {
		return HealthCheck{}, errors.New("ecs.healthCheck.command: expected CMD or CMD-SHELL and a command")
	}
	if h.IntervalSeconds < 5 || h.IntervalSeconds > 300 {
		return HealthCheck{}, errors.New("ecs.healthCheck.intervalSeconds: must be between 5 and 300")
	}
	if h.TimeoutSeconds < 2 || h.TimeoutSeconds > 60 {
		return HealthCheck{}, errors.New("ecs.healthCheck.timeoutSeconds: must be between 2 and 60")
	}
	if h.Retries < 1 || h.Retries > 10 {
		return HealthCheck{}, errors.New("ecs.healthCheck.retries: must be between 1 and 10")
	}
	if h.StartPeriodSeconds < 0 || h.StartPeriodSeconds > 300 {
		return HealthCheck{}, errors.New("ecs.healthCheck.startPeriodSeconds: must be between 0 and 300")
	}
	return HealthCheck{
		Command:     h.Command,
		Interval:    time.Duration(h.IntervalSeconds) * time.Second,
		Timeout:     time.Duration(h.TimeoutSeconds) * time.Second,
		Retries:     h.Retries,
		StartPeriod: time.Duration(h.StartPeriodSeconds) * time.Second,
	}, nil
}
func loadConfig(ctx *pulumi.Context) (deploymentConfig, error) {
	d, err := loadMetadata(ctx)
	if err != nil {
		return d, err
	}
	c := pc.New(ctx, "")
	if d.BaseStack, err = c.Try("baseStack"); err != nil || strings.TrimSpace(d.BaseStack) == "" {
		return d, fmt.Errorf("baseStack is required: %v", err)
	}
	if d.Image, err = c.Try("image"); err != nil {
		return d, err
	}
	var network vpcSettings
	if err = c.TryObject("vpc", &network); err != nil {
		return d, fmt.Errorf("vpc: %w", err)
	}
	if err = network.apply(&d); err != nil {
		return d, err
	}
	if err = d.Network.Validate(); err != nil {
		return d, err
	}
	if strings.TrimSpace(d.AvailabilityZone) == "" {
		return d, errors.New("vpc.availabilityZone is required")
	}
	var task ecsSettings
	if err = c.TryObject("ecs", &task); err != nil {
		return d, fmt.Errorf("ecs: %w", err)
	}
	if err = task.apply(&d.Task); err != nil {
		return d, err
	}
	if err = d.Task.Resources.Validate(); err != nil {
		return d, err
	}
	var scaling autoscalingSettings
	if err = c.TryObject("autoscaling", &scaling); err != nil {
		return d, fmt.Errorf("autoscaling: %w", err)
	}
	if err = scaling.apply(&d.Scaling); err != nil {
		return d, err
	}
	if d.Scaling.MinTasks < 0 {
		return d, errors.New("autoscaling.minTasks must be non-negative")
	}
	if d.Scaling.MaxTasks < 1 || d.Scaling.MaxTasks < d.Scaling.MinTasks || d.Scaling.MaxTasks > math.MaxInt32 {
		return d, errors.New("autoscaling.maxTasks must be positive, at least minTasks, and fit an AWS 32-bit integer")
	}
	if d.Scaling.BacklogPerWorker < 1 {
		return d, errors.New("autoscaling.backlogPerWorker must be positive")
	}

	return d, nil
}
