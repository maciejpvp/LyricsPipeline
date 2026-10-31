package ecs

import "fmt"

type CPUUnits int
type MemoryMiB int

const CPUUnitsPerVCPU CPUUnits = 1024
const MiBPerGiB MemoryMiB = 1024

type Resources struct {
	CPU    CPUUnits
	Memory MemoryMiB
}

// Validate follows the Linux Fargate task-size table:
// https://docs.aws.amazon.com/AmazonECS/latest/developerguide/fargate-tasks-services.html
func (r Resources) Validate() error {
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
	return fmt.Errorf("ecs.cpuUnits/memoryMiB: unsupported Fargate combination %d CPU units and %d MiB", r.CPU, r.Memory)
}
