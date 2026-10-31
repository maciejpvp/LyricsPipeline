package main

import (
	"strings"
	"testing"

	"LyricsPipeline/internal/configtest"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func TestMissingConfigFailsBeforeRegistration(t *testing.T) {
	for _, key := range []string{"name", "tags", "baseStack", "transcription"} {
		t.Run(key, func(t *testing.T) {
			values := configtest.ValuesAt(t, "Pulumi.yaml")
			values["LyricsPipeline-Transcription:baseStack"] = "org/LyricsPipeline/dev"
			delete(values, "LyricsPipeline-Transcription:"+key)
			m := &infraMocks{}
			err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Transcription", "test", m), func(info *pulumi.RunInfo) { info.Config = values })
			if err == nil || len(m.AllResources) != 0 {
				t.Fatalf("missing %s: registered %d resources; error=%v", key, len(m.AllResources), err)
			}
		})
	}
}
func TestProviderMismatchFailsBeforeRegistration(t *testing.T) {
	m := &infraMocks{ProviderRegion: "us-east-1"}
	err := pulumi.RunErr(deploy, pulumi.WithMocks("LyricsPipeline-Transcription", "test", m), withStackConfig(t, nil))
	if err == nil || !strings.Contains(err.Error(), "region") || len(m.AllResources) != 0 {
		t.Fatalf("provider mismatch: %v", err)
	}
}
