package main

import (
	"strings"
	"testing"

	"LyricsPipeline/internal/configtest"
	"LyricsPipeline/internal/infratest"

	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func nativeValues(t *testing.T) map[string]string { return configtest.ValuesAt(t, "Pulumi.yaml") }
func loadForTest(t *testing.T, values map[string]string) (deploymentConfig, error) {
	t.Helper()
	var got deploymentConfig
	var loadErr error
	err := pulumi.RunErr(func(ctx *pulumi.Context) error { got, loadErr = loadConfig(ctx); return nil }, pulumi.WithMocks("LyricsPipeline", "test", &infratest.Mocks{}), func(info *pulumi.RunInfo) { info.Config = values })
	if err != nil {
		t.Fatal(err)
	}
	return got, loadErr
}
func TestNativeConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, key, value, field string }{
		{name: "central values"},
		{"one receive", "sqs", `{"maxReceiveCount":1}`, "maxReceiveCount"},
		{"zero receives", "sqs", `{"maxReceiveCount":0}`, "maxReceiveCount"},
		{"two receives", "sqs", `{"maxReceiveCount":2}`, ""},
		{"excess receives", "sqs", `{"maxReceiveCount":1001}`, "maxReceiveCount"},
		{"null tags", "tags", `null`, "tags"},
		{"empty name", "name", " ", "name"},
		{"zero visibility", "sqs", `{"visibilitySeconds":0}`, "visibilitySeconds"},
		{"excess visibility", "sqs", `{"visibilitySeconds":43201}`, "visibilitySeconds"},
		{"visibility boundary", "sqs", `{"visibilitySeconds":43200}`, ""},
		{"legacy deployment", "deployment", `{}`, "legacy configuration"},
		{"legacy image", "deploymentImage", "registry/image:tag", "legacy configuration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			values := nativeValues(t)
			if tc.key != "" {
				key := "LyricsPipeline:" + tc.key
				if tc.key == "name" || tc.key == "image" || tc.key == "deployment" || tc.key == "deploymentImage" || tc.key == "tags" {
					values[key] = tc.value
				} else {
					configtest.EditObject(t, values, key, tc.value)
				}
			}
			_, err := loadForTest(t, values)
			if tc.field == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("got %v, expected error for %s", err, tc.field)
			}
		})
	}
}
func TestNativeStackFile(t *testing.T) {
	if _, err := loadForTest(t, configtest.StackValuesAt(t, "Pulumi.yaml", "dev")); err != nil {
		t.Fatal(err)
	}
}
func TestMissingConfigurationHasNoFallback(t *testing.T) {
	for _, key := range []string{"name", "tags", "s3", "sqs"} {
		t.Run(key, func(t *testing.T) {
			values := nativeValues(t)
			delete(values, "LyricsPipeline:"+key)
			if _, err := loadForTest(t, values); err == nil {
				t.Fatal("missing configuration accepted")
			}
		})
	}
	for _, key := range []string{"s3", "sqs"} {
		t.Run("partial_"+key, func(t *testing.T) {
			values := nativeValues(t)
			values["LyricsPipeline:"+key] = `{}`
			if _, err := loadForTest(t, values); err == nil || !strings.Contains(err.Error(), "missing required field") {
				t.Fatalf("partial override accepted: %v", err)
			}
		})
	}
}
func TestOperationalLimits(t *testing.T) {
	for _, tc := range []struct{ key, patch, field string }{
		{"sqs", `{"longPollWaitSeconds":21}`, "longPollWaitSeconds"},
		{"sqs", `{"deadLetterRetentionDays":15}`, "deadLetterRetentionDays"},
		{"s3", `{"inputPrefix":""}`, "inputPrefix"},
		{"s3", `{"outputPrefix":"input/results"}`, "outputPrefix"},
	} {
		t.Run(tc.field, func(t *testing.T) {
			values := nativeValues(t)
			configtest.EditObject(t, values, "LyricsPipeline:"+tc.key, tc.patch)
			_, err := loadForTest(t, values)
			if err == nil || !strings.Contains(err.Error(), tc.field) {
				t.Fatalf("expected %s error, got %v", tc.field, err)
			}
		})
	}
}
