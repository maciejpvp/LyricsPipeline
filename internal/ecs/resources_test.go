package ecs

import "testing"

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
		if err := (Resources{CPU: tc.cpu, Memory: tc.memory}).Validate(); (err == nil) != tc.valid {
			t.Errorf("CPU=%d memory=%d: %v, valid=%v", tc.cpu, tc.memory, err, tc.valid)
		}
	}
}
