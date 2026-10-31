package autoscaling

import "time"

type Settings struct {
	MinTasks         int
	MaxTasks         int
	IdleWindow       time.Duration
	BacklogPerWorker int
	ScaleOutCooldown time.Duration
	ScaleInCooldown  time.Duration
}
