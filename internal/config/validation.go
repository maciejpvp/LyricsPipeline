package config

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"strings"
)

func (d Deployment) Validate() error {
	var problems []error
	for _, field := range []struct{ name, value string }{
		{"name", d.Name}, {"region", d.Region}, {"availabilityZone", d.AvailabilityZone},
	} {
		if strings.TrimSpace(field.value) == "" {
			problems = append(problems, fmt.Errorf("deployment.%s: must not be empty", field.name))
		}
	}
	if d.Scaling.MinTasks < 0 {
		problems = append(problems, errors.New("deployment.minTasks: must be non-negative"))
	}
	if d.Scaling.MaxTasks < 1 || d.Scaling.MaxTasks < d.Scaling.MinTasks || d.Scaling.MaxTasks > math.MaxInt32 {
		problems = append(problems, errors.New("deployment.maxTasks: must be positive, at least minTasks, and fit an AWS 32-bit integer"))
	}
	if d.Queue.MaxReceiveCount < 2 || d.Queue.MaxReceiveCount > 1000 {
		problems = append(problems, errors.New("deployment.maxReceiveCount: must be between 2 and 1000"))
	}
	if d.Scaling.BacklogPerWorker < 1 {
		problems = append(problems, errors.New("deployment.backlogPerWorker: must be positive"))
	}
	problems = append(problems, d.Task.Resources.Validate(), d.Network.Validate())
	return errors.Join(problems...)
}

// Validate follows the Linux Fargate task-size table:
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/fargate-tasks-services.html
func (r TaskResources) Validate() error {
	type memoryRange struct {
		cpu                    CPUUnits
		minimum, maximum, step MemoryMiB
	}
	ranges := []memoryRange{
		{512, 1 * MiBPerGiB, 4 * MiBPerGiB, MiBPerGiB},
		{1024, 2 * MiBPerGiB, 8 * MiBPerGiB, MiBPerGiB},
		{2048, 4 * MiBPerGiB, 16 * MiBPerGiB, MiBPerGiB},
		{4096, 8 * MiBPerGiB, 30 * MiBPerGiB, MiBPerGiB},
		{8192, 16 * MiBPerGiB, 60 * MiBPerGiB, 4 * MiBPerGiB},
		{16384, 32 * MiBPerGiB, 120 * MiBPerGiB, 8 * MiBPerGiB},
	}
	if r.CPU == 256 && (r.Memory == 512 || r.Memory == MiBPerGiB || r.Memory == 2*MiBPerGiB) {
		return nil
	}
	if r.CPU == 32768 && (r.Memory == 60*MiBPerGiB || r.Memory == 120*MiBPerGiB || r.Memory == 244*MiBPerGiB) {
		return nil
	}
	for _, allowed := range ranges {
		if r.CPU == allowed.cpu && r.Memory >= allowed.minimum && r.Memory <= allowed.maximum && (r.Memory-allowed.minimum)%allowed.step == 0 {
			return nil
		}
	}
	return fmt.Errorf("deployment.taskCpu/taskMemory: unsupported Fargate combination %d CPU units and %d MiB", r.CPU, r.Memory)
}

func (n Network) Validate() error {
	for _, field := range []struct {
		name   string
		prefix netip.Prefix
	}{
		{"vpc", n.VPC}, {"publicSubnet", n.PublicSubnet}, {"privateSubnet", n.PrivateSubnet}, {"internet", n.Internet},
	} {
		if !field.prefix.IsValid() || !field.prefix.Addr().Is4() || field.prefix != field.prefix.Masked() {
			return fmt.Errorf("network.%s: expected a canonical IPv4 CIDR", field.name)
		}
	}
	for _, subnet := range []netip.Prefix{n.PublicSubnet, n.PrivateSubnet} {
		if subnet.Bits() <= n.VPC.Bits() || !n.VPC.Contains(subnet.Addr()) {
			return errors.New("network: subnets must be contained within the VPC")
		}
	}
	if n.PublicSubnet.Overlaps(n.PrivateSubnet) {
		return errors.New("network: public and private subnets must not overlap")
	}
	return nil
}
