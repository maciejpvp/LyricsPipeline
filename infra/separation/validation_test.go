package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"LyricsPipeline/internal/configtest"

	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestMissingConfigFailsBeforeRegistration(t *testing.T) {
	for _, key := range []string{"name", "tags", "image", "baseStack", "vpc", "ecs", "autoscaling"} {
		t.Run(key, func(t *testing.T) {
			values := configtest.ValuesAt(t, "Pulumi.yaml")
			values["LyricsPipeline-Separation:baseStack"] = "org/LyricsPipeline/dev"
			delete(values, "LyricsPipeline-Separation:"+key)
			m := &infraMocks{}
			err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), func(info *pulumi.RunInfo) { info.Config = values })
			if err == nil || len(m.AllResources) != 0 {
				t.Fatalf("missing %s: registered %d resources; error=%v", key, len(m.AllResources), err)
			}
		})
	}
}
func TestProviderMismatchFailsBeforeRegistration(t *testing.T) {
	m := &infraMocks{ProviderRegion: "us-east-1"}
	err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, nil))
	if err == nil || !strings.Contains(err.Error(), "region") || len(m.AllResources) != 0 {
		t.Fatalf("provider mismatch: %v", err)
	}
}

func TestBaseContractFailureStopsResources(t *testing.T) {
	for _, tc := range []struct {
		key     resource.PropertyKey
		value   any
		missing bool
	}{
		{key: "accountId", value: "987654321098"},
		{key: "region", value: "us-east-1"},
		{key: "contractVersion", value: 2},
		{key: "queueArn", missing: true},
		{key: "mediaBucket", value: ""},
		{key: "outputPrefix", value: true},
	} {
		for _, preview := range []bool{false, true} {
			// Missing outputs remain unknown during preview, as in the SDK.
			if preview && tc.missing {
				continue
			}
			for _, delayed := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/preview=%t/delayed=%t", tc.key, preview, delayed), func(t *testing.T) {
					outputs := baseOutputs()
					if tc.missing {
						delete(outputs, tc.key)
					} else {
						outputs[tc.key] = resource.NewPropertyValue(tc.value)
					}
					m := &infraMocks{ReferenceOutputs: outputs}
					started, release := make(chan struct{}), make(chan struct{})
					if delayed {
						m.BeforeResource = func(args pulumi.MockResourceArgs) {
							if args.TypeToken == "pulumi:pulumi:StackReference" {
								close(started)
								<-release
							}
						}
					}
					options := []pulumi.RunOption{pulumi.WithMocks("LyricsPipeline-Separation", "test", m), withStackConfig(t, nil), func(info *pulumi.RunInfo) { info.DryRun = preview }}
					returned, done := make(chan struct{}), make(chan error, 1)
					go func() {
						done <- pulumi.RunErr(func(ctx *pulumi.Context) error {
							err := deploy(ctx)
							close(returned)
							return err
						}, options...)
					}()
					if delayed {
						select {
						case <-started:
						case <-time.After(10 * time.Second):
							close(release)
							t.Fatal("stack read did not start")
						}
						select {
						case <-returned:
							t.Error("deployment returned before the base stack read completed")
						case <-time.After(20 * time.Millisecond):
						}
						close(release)
					}
					select {
					case err := <-done:
						if err == nil || !strings.Contains(err.Error(), string(tc.key)) {
							t.Errorf("expected base contract error for %s: %v", tc.key, err)
						}
					case <-time.After(10 * time.Second):
						t.Fatal("deployment did not finish")
					}
					if len(m.AllResources) != 1 {
						t.Fatalf("invalid base registered %d resources; only StackReference is allowed", len(m.AllResources))
					}
					for _, r := range m.AllResources {
						if r.TypeToken != "pulumi:pulumi:StackReference" {
							t.Errorf("invalid base registered %s", r.TypeToken)
						}
					}
				})
			}
		}
	}
}
