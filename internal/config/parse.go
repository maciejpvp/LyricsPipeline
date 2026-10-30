package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// deploymentJSON preserves the existing external contract; units are explicit here.
type deploymentJSON struct {
	Name                    string            `json:"name"`
	Region                  string            `json:"region"`
	AvailabilityZone        string            `json:"availabilityZone"`
	Image                   string            `json:"image"`
	TaskCPU                 string            `json:"taskCpu"`
	TaskMemory              string            `json:"taskMemory"`
	MinTasks                int               `json:"minTasks"`
	MaxTasks                int               `json:"maxTasks"`
	VisibilitySeconds       int               `json:"visibilitySeconds"`
	MaxReceiveCount         int               `json:"maxReceiveCount"`
	IdleMinutes             int               `json:"idleMinutes"`
	BacklogPerWorker        int               `json:"backlogPerWorker"`
	ScaleOutCooldownSeconds int               `json:"scaleOutCooldownSeconds"`
	Tags                    map[string]string `json:"tags"`
}

func deploymentDefaults(d Deployment) deploymentJSON {
	return deploymentJSON{
		Name:                    d.Name,
		Region:                  d.Region,
		AvailabilityZone:        d.AvailabilityZone,
		Image:                   d.Image,
		TaskCPU:                 strconv.Itoa(int(d.Task.Resources.CPU)),
		TaskMemory:              strconv.Itoa(int(d.Task.Resources.Memory)),
		MinTasks:                d.Scaling.MinTasks,
		MaxTasks:                d.Scaling.MaxTasks,
		VisibilitySeconds:       int(d.Queue.VisibilityTimeout / time.Second),
		MaxReceiveCount:         d.Queue.MaxReceiveCount,
		IdleMinutes:             int(d.Scaling.IdleWindow / time.Minute),
		BacklogPerWorker:        d.Scaling.BacklogPerWorker,
		ScaleOutCooldownSeconds: int(d.Scaling.ScaleOutCooldown / time.Second),
		Tags:                    d.Tags,
	}
}

func Parse(raw, imageOverride string) (Deployment, error) {
	d := Defaults()
	wire := deploymentDefaults(d)
	if raw != "" {
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.DisallowUnknownFields()
		document := &wire
		if err := decoder.Decode(&document); err != nil {
			return Deployment{}, fmt.Errorf("deployment: %w", err)
		}
		if document == nil {
			return Deployment{}, errors.New("deployment: expected a JSON object, got null")
		}
		var trailing json.RawMessage
		if err := decoder.Decode(&trailing); err != io.EOF {
			return Deployment{}, errors.New("deployment: expected exactly one JSON object")
		}
	}
	cpu, cpuErr := strconv.Atoi(wire.TaskCPU)
	memory, memoryErr := strconv.Atoi(wire.TaskMemory)
	if cpuErr != nil {
		return Deployment{}, fmt.Errorf("deployment.taskCpu: expected integer CPU units: %w", cpuErr)
	}
	if memoryErr != nil {
		return Deployment{}, fmt.Errorf("deployment.taskMemory: expected integer MiB: %w", memoryErr)
	}
	// Check bounds before converting external integers to nanosecond durations.
	if wire.VisibilitySeconds < 1 || wire.VisibilitySeconds > int((12*time.Hour)/time.Second) {
		return Deployment{}, errors.New("deployment.visibilitySeconds: must be between 1 and 43200")
	}
	// With one-minute metrics, CloudWatch permits at most one day of evaluation.
	if wire.IdleMinutes < 1 || wire.IdleMinutes > int((24*time.Hour)/time.Minute) {
		return Deployment{}, errors.New("deployment.idleMinutes: must be between 1 and 1440")
	}
	if wire.ScaleOutCooldownSeconds < 1 || wire.ScaleOutCooldownSeconds > math.MaxInt32 {
		return Deployment{}, errors.New("deployment.scaleOutCooldownSeconds: must be a positive AWS 32-bit integer")
	}
	d.Name, d.Region, d.AvailabilityZone, d.Image, d.Tags = wire.Name, wire.Region, wire.AvailabilityZone, wire.Image, wire.Tags
	if imageOverride != "" {
		d.Image = imageOverride
	}
	d.Task.Resources = TaskResources{
		CPU:    CPUUnits(cpu),
		Memory: MemoryMiB(memory),
	}
	d.Queue.VisibilityTimeout = time.Duration(wire.VisibilitySeconds) * time.Second
	d.Queue.MaxReceiveCount = wire.MaxReceiveCount
	d.Scaling.MinTasks, d.Scaling.MaxTasks = wire.MinTasks, wire.MaxTasks
	d.Scaling.IdleWindow = time.Duration(wire.IdleMinutes) * time.Minute
	d.Scaling.BacklogPerWorker = wire.BacklogPerWorker
	d.Scaling.ScaleOutCooldown = time.Duration(wire.ScaleOutCooldownSeconds) * time.Second
	if err := d.Validate(); err != nil {
		return Deployment{}, err
	}
	return d, nil
}
