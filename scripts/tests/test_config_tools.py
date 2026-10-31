import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location("migration", ROOT / "scripts/migrate-pulumi-config.py")
migration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(migration)


def legacy_config():
    return {
        "LyricsPipeline:deployment": {"value": json.dumps({
            "name": "custom-audio", "region": "eu-central-1", "availabilityZone": "eu-central-1b",
            "taskCpu": "1024", "taskMemory": "4096", "image": "registry/old:tag",
            "minTasks": 0, "maxTasks": 7, "idleMinutes": 9, "backlogPerWorker": 3,
            "visibilitySeconds": 123, "maxReceiveCount": 8, "scaleOutCooldownSeconds": 600,
            "tags": {"Owner": "team"},
        }), "secret": False},
        "LyricsPipeline:deploymentImage": {"value": "registry/new@sha256:digest", "secret": False},
        "aws:region": {"value": "eu-central-1", "secret": False},
        "other:secret": {"value": "[secret]", "secret": True},
    }


class MigrationTests(unittest.TestCase):
    def test_preserves_custom_values_and_published_image(self):
        source = legacy_config()
        original = json.dumps(source)
        changes = migration.migration_values(source)
        self.assertEqual(changes["LyricsPipeline:ecs"]["objectValue"], {
            "cpuUnits": 1024, "memoryMiB": 4096,
        })
        self.assertEqual(changes["LyricsPipeline:image"]["value"], "registry/new@sha256:digest")
        self.assertEqual(changes["LyricsPipeline:autoscaling"]["objectValue"]["minTasks"], 0)
        self.assertEqual(changes["LyricsPipeline:autoscaling"]["objectValue"]["maxTasks"], 7)
        self.assertEqual(changes["LyricsPipeline:sqs"]["objectValue"]["visibilitySeconds"], 123)
        self.assertEqual(changes["LyricsPipeline:tags"]["objectValue"], {"Owner": "team"})
        self.assertNotIn("other:secret", changes)
        self.assertEqual(json.dumps(source), original)

    def test_omitted_legacy_fields_are_not_invented(self):
        changes = migration.migration_values({"LyricsPipeline:deployment": {"value": "{}"}})
        self.assertEqual(changes["aws:region"]["value"], "eu-central-1")
        self.assertEqual(changes["LyricsPipeline:vpc"]["objectValue"], {"availabilityZone": "eu-central-1a"})
        self.assertEqual(changes["LyricsPipeline:ecs"]["objectValue"], {})
        self.assertEqual(changes["LyricsPipeline:autoscaling"]["objectValue"], {})

    def test_shared_values_are_not_copied_to_stack(self):
        shared = {
            "aws:region": migration.native_entry("eu-central-1"),
            "LyricsPipeline:vpc": migration.native_entry({"availabilityZone": "eu-central-1a"}),
            "LyricsPipeline:ecs": migration.native_entry({"cpuUnits": 2048, "memoryMiB": 8192}),
            "LyricsPipeline:sqs": migration.native_entry({"visibilitySeconds": 900}),
            "LyricsPipeline:autoscaling": migration.native_entry({"maxTasks": 2}),
            "LyricsPipeline:image": migration.native_entry(""),
        }
        current = shared | {"LyricsPipeline:deployment": migration.native_entry("{}")}
        self.assertEqual(migration.migration_values(current, shared), {})
        current["LyricsPipeline:deployment"] = migration.native_entry('{"taskCpu":"4096"}')
        changes = migration.migration_values(current, shared)
        self.assertEqual(changes, {"LyricsPipeline:ecs": migration.native_entry({
            "cpuUnits": 4096, "memoryMiB": 8192,
        })})

    def test_refuses_conflicts_and_invalid_inputs(self):
        for key, entry in [
            ("aws:region", {"value": "us-east-1"}),
            ("LyricsPipeline:ecs", {"objectValue": {"cpuUnits": 8192}}),
            ("LyricsPipeline:deployment", {"value": "null"}),
            ("LyricsPipeline:deployment", {"value": '{"unknown":1}'}),
            ("LyricsPipeline:deployment", {"value": '{"taskCpu":"two"}'}),
            ("LyricsPipeline:deployment", {"value": '{"minTasks":true}'}),
            ("LyricsPipeline:deployment", {"value": "[secret]", "secret": True}),
        ]:
            with self.subTest(key=key, entry=entry):
                values = legacy_config()
                values[key] = entry
                with self.assertRaises(ValueError):
                    migration.migration_values(values)

    def test_restores_original_file_when_pulumi_write_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            config_file = Path(directory) / "Pulumi.test.yaml"
            config_file.write_text("original config\n")

            def fail_write(args, **kwargs):
                if args[1:3] == ["config", "--json"]:
                    if Path(args[args.index("--config-file")+1]).name=="project-values.yaml":
                        shared={f"LyricsPipeline:{key}":migration.native_entry({}) for key in ("vpc","s3","ecs","sqs","autoscaling")}
                        return subprocess.CompletedProcess(args,0,json.dumps(shared),"")
                    return subprocess.CompletedProcess(args, 0, json.dumps(legacy_config()), "")
                config_file.write_text("partial write\n")
                raise subprocess.CalledProcessError(1, args, stderr="simulated write failure")

            with patch.object(migration.subprocess, "run", side_effect=fail_write):
                with self.assertRaises(subprocess.CalledProcessError):
                    migration.migrate("test", config_file)
            self.assertEqual(config_file.read_text(), "original config\n")

    def test_already_migrated_configuration_is_unchanged(self):
        result = subprocess.CompletedProcess([], 0, '{"LyricsPipeline:ecs":{"objectValue":{}}}', "")
        with patch.object(migration.subprocess, "run", return_value=result) as run:
            self.assertFalse(migration.migrate("test", Path("unused")))
        self.assertEqual(run.call_count, 1)


FAKE_TOOL = '''#!/usr/bin/env python3
import json, os, pathlib, sys
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
with open(os.environ["CONFIG_TOOL_TEST_LOG"], "a") as log:
    log.write(json.dumps([name, *args]) + "\\n")
if name == "pulumi":
    if args[:3] == ["stack", "output", "repositoryUrl"]:
        print("123456789012.dkr.ecr.eu-central-1.amazonaws.com/audio")
    elif args[:3] == ["config", "get", "aws:region"]:
        print(os.environ.get("CONFIG_TOOL_TEST_REGION", "eu-central-1"))
elif name == "aws":
    if args[:2] == ["ecr", "get-login-password"]:
        print("test-password")
    elif args[:2] == ["ecr", "describe-images"]:
        print("sha256:test-digest")
elif name == "docker" and args and args[0] == "login":
    sys.stdin.read()
'''


class PublisherTests(unittest.TestCase):
    def run_publisher(self, region):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            log = folder / "commands.jsonl"
            for name in ("pulumi", "aws", "docker"):
                tool = folder / name
                tool.write_text(FAKE_TOOL)
                tool.chmod(0o755)
            env = os.environ.copy()
            env.update(PATH=f"{folder}:{env['PATH']}", GIT_SHA="test-revision", AWS_REGION="us-east-1",
                       CONFIG_TOOL_TEST_LOG=str(log), CONFIG_TOOL_TEST_REGION=region)
            result = subprocess.run(["bash", str(ROOT / "scripts/publish-vocal-extractor-image.sh")],
                                    env=env, cwd=folder, text=True, capture_output=True)
            commands = [json.loads(line) for line in log.read_text().splitlines()]
            return result, commands

    def test_uses_stack_region_and_updates_one_image_setting(self):
        result, commands = self.run_publisher("eu-central-1")
        self.assertEqual(result.returncode, 0, result.stderr)
        updates = [args for args in commands if args[:3] == ["pulumi", "config", "set"]]
        self.assertEqual(updates, [["pulumi", "config", "set", "image",
                                  "123456789012.dkr.ecr.eu-central-1.amazonaws.com/audio@sha256:test-digest", "--stack", "dev"]])
        for args in commands:
            if args[0] == "aws":
                self.assertEqual(args[args.index("--region") + 1], "eu-central-1")

    def test_missing_stack_region_stops_before_publish(self):
        result, commands = self.run_publisher("")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("aws:region", result.stderr)
        self.assertFalse(any(args[0] in ("aws", "docker") for args in commands))


if __name__ == "__main__":
    unittest.main()
