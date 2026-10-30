// Package config parses the public deployment JSON into validated infrastructure settings.
// It has no dependency on Pulumi or on AWS resource constructors.
package config

import (
	"net/netip"
	"time"
)

type CPUUnits int
type MemoryMiB int

const CPUUnitsPerVCPU CPUUnits = 1024
const MiBPerGiB MemoryMiB = 1024

type Deployment struct {
	Name             string
	Region           string
	AvailabilityZone string
	Image            string
	Tags             map[string]string
	Network          Network
	Queue            Queue
	Task             Task
	Scaling          Scaling
	Objects          ObjectLayout
}

type Network struct {
	VPC           netip.Prefix
	PublicSubnet  netip.Prefix
	PrivateSubnet netip.Prefix
	Internet      netip.Prefix
}

type Queue struct {
	VisibilityTimeout   time.Duration
	MaxReceiveCount     int
	LongPollWait        time.Duration
	DeadLetterRetention time.Duration
}

type TaskResources struct {
	CPU    CPUUnits
	Memory MemoryMiB
}

type Task struct {
	Resources    TaskResources
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

type Scaling struct {
	MinTasks         int
	MaxTasks         int
	IdleWindow       time.Duration
	BacklogPerWorker int
	ScaleOutCooldown time.Duration
	ScaleInCooldown  time.Duration
}

type ObjectPrefix string

func (p ObjectPrefix) NotificationPrefix() string { return string(p) + "/" }

type ObjectLayout struct {
	Input  ObjectPrefix
	Output ObjectPrefix
}
