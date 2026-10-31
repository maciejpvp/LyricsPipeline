package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/netip"
	"reflect"
	"strings"
	"time"
)

// Wire structs describe the configuration owned by Pulumi. Every field is
// required, including fields whose zero value is valid. Go never fills omissions.
type vpcSettings struct {
	AvailabilityZone  string `json:"availabilityZone"`
	CIDR              string `json:"cidr"`
	PublicSubnetCIDR  string `json:"publicSubnetCIDR"`
	PrivateSubnetCIDR string `json:"privateSubnetCIDR"`
	InternetCIDR      string `json:"internetCIDR"`
}
type s3Settings struct {
	InputPrefix  ObjectPrefix `json:"inputPrefix"`
	OutputPrefix ObjectPrefix `json:"outputPrefix"`
}
type ecsSettings struct {
	CPUUnits           CPUUnits            `json:"cpuUnits"`
	MemoryMiB          MemoryMiB           `json:"memoryMiB"`
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
type sqsSettings struct {
	VisibilitySeconds       int `json:"visibilitySeconds"`
	MaxReceiveCount         int `json:"maxReceiveCount"`
	LongPollWaitSeconds     int `json:"longPollWaitSeconds"`
	DeadLetterRetentionDays int `json:"deadLetterRetentionDays"`
}
type autoscalingSettings struct {
	MinTasks                int `json:"minTasks"`
	MaxTasks                int `json:"maxTasks"`
	IdleMinutes             int `json:"idleMinutes"`
	BacklogPerWorker        int `json:"backlogPerWorker"`
	ScaleOutCooldownSeconds int `json:"scaleOutCooldownSeconds"`
	ScaleInCooldownSeconds  int `json:"scaleInCooldownSeconds"`
}

func (s *vpcSettings) UnmarshalJSON(data []byte) error {
	type fields vpcSettings
	return decodeObject(data, (*fields)(s))
}
func (s *s3Settings) UnmarshalJSON(data []byte) error {
	type fields s3Settings
	return decodeObject(data, (*fields)(s))
}
func (s *ecsSettings) UnmarshalJSON(data []byte) error {
	type fields ecsSettings
	return decodeObject(data, (*fields)(s))
}
func (s *healthCheckSettings) UnmarshalJSON(data []byte) error {
	type fields healthCheckSettings
	return decodeObject(data, (*fields)(s))
}
func (s *sqsSettings) UnmarshalJSON(data []byte) error {
	type fields sqsSettings
	return decodeObject(data, (*fields)(s))
}
func (s *autoscalingSettings) UnmarshalJSON(data []byte) error {
	type fields autoscalingSettings
	return decodeObject(data, (*fields)(s))
}

// Reflection is limited to this JSON boundary: derive required names from the
// typed struct tags rather than maintain a second, drifting list of schema keys.
func decodeObject(data []byte, target any) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return errors.New("expected a configuration object, got null")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	schema := reflect.TypeOf(target).Elem()
	for i := 0; i < schema.NumField(); i++ {
		name := strings.Split(schema.Field(i).Tag.Get("json"), ",")[0]
		value, ok := fields[name]
		if !ok {
			return fmt.Errorf("missing required field %q; configure the complete object in Pulumi.yaml or the stack override", name)
		}
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%s: null is not a valid setting", name)
		}
	}
	return nil
}

func (s vpcSettings) apply(d *Deployment) error {
	d.AvailabilityZone = s.AvailabilityZone
	for _, field := range []struct {
		name, value string
		target      *netip.Prefix
	}{
		{"cidr", s.CIDR, &d.Network.VPC}, {"publicSubnetCIDR", s.PublicSubnetCIDR, &d.Network.PublicSubnet},
		{"privateSubnetCIDR", s.PrivateSubnetCIDR, &d.Network.PrivateSubnet}, {"internetCIDR", s.InternetCIDR, &d.Network.Internet},
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
	task.Resources = TaskResources{CPU: s.CPUUnits, Memory: s.MemoryMiB}
	task.StopTimeout = time.Duration(s.StopTimeoutSeconds) * time.Second
	task.LogRetention = time.Duration(s.LogRetentionDays) * 24 * time.Hour
	task.HealthCheck = health
	return nil
}

func (s sqsSettings) apply(queue *Queue) error {
	const maximumVisibility = 12 * time.Hour
	if s.VisibilitySeconds < 1 || s.VisibilitySeconds > int(maximumVisibility/time.Second) {
		return errors.New("sqs.visibilitySeconds: must be between 1 and 43200")
	}
	if s.LongPollWaitSeconds < 0 || s.LongPollWaitSeconds > 20 {
		return errors.New("sqs.longPollWaitSeconds: must be between 0 and 20")
	}
	if s.DeadLetterRetentionDays < 1 || s.DeadLetterRetentionDays > 14 {
		return errors.New("sqs.deadLetterRetentionDays: must be between 1 and 14")
	}
	queue.VisibilityTimeout = time.Duration(s.VisibilitySeconds) * time.Second
	queue.MaxReceiveCount = s.MaxReceiveCount
	queue.LongPollWait = time.Duration(s.LongPollWaitSeconds) * time.Second
	queue.DeadLetterRetention = time.Duration(s.DeadLetterRetentionDays) * 24 * time.Hour
	return nil
}

func (s autoscalingSettings) apply(scaling *Scaling) error {
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
