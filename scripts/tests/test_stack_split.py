import importlib.util
import json
import subprocess
import tempfile
from unittest.mock import patch
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("split", Path(__file__).parents[1] / "split-pulumi-stacks.py")
split = importlib.util.module_from_spec(spec)
spec.loader.exec_module(split)


class StackSplitTests(unittest.TestCase):
    def test_moves_only_complete_separation_components(self):
        rows = [{"type": kind, "urn": f"urn:{kind}"} for kind in split.SEPARATION_TYPES]
        rows += [{"type": "custom:storage:Storage", "urn": "bucket"}, {"type": "lyrics:queue:Queue", "urn": "queue"}]
        roots = split.move_roots({"deployment": {"resources": rows}})
        self.assertEqual(len(roots), 6)
        self.assertNotIn("bucket", roots)
        self.assertNotIn("queue", roots)
        for invalid in ([], rows[:1], rows + [rows[0]]):
            with self.assertRaises(ValueError):
                split.move_roots({"deployment": {"resources": invalid}})

    def test_configuration_preserves_source_values_and_rejects_secrets(self):
        source = {"LyricsPipeline:image": {"value": "registry@sha256:123"},
                  "LyricsPipeline:ecs": {"objectValue": {"cpuUnits": 4096}},
                  "other:secret": {"secret": True}}
        result = split.config_changes(source, "org/LyricsPipeline/prod", "org/LyricsPipeline-Separation/prod")
        self.assertEqual(result["LyricsPipeline-Separation:image"], source["LyricsPipeline:image"])
        self.assertEqual(result["LyricsPipeline-Separation:ecs"], source["LyricsPipeline:ecs"])
        self.assertNotIn("other:secret", result)
        source["LyricsPipeline:image"]["secret"] = True
        with self.assertRaises(ValueError):
            split.config_changes(source, "base", "sep")


class StackSplitPathTests(unittest.TestCase):
    def exercise(self, apply):
        source = "org/LyricsPipeline/dev"
        destination = "org/LyricsPipeline-Separation/dev"
        source_state = {"deployment": {"resources": [
            {"type": kind, "urn": f"urn:{kind}"} for kind in split.SEPARATION_TYPES]}}
        calls = []

        def command(args, **kwargs):
            calls.append(args)
            folder = Path(args[args.index("-C") + 1])
            if "export" in args:
                payload = source_state if folder == split.BASE else {"deployment": {"resources": []}}
            elif args[3:5] == ["config", "--json"]:
                payload = {"LyricsPipeline:image": {"value": "registry@sha256:fixture"}}
            else:
                payload = {}
            return subprocess.CompletedProcess(args, 0, json.dumps(payload), "")

        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            base, separation = root / "infra/base", root / "infra/separation"
            for project in (base, separation):
                project.mkdir(parents=True)
                (project / "Pulumi.dev.yaml").write_text("config: {}\n")
            argv = ["split", "--source", source, "--dest", destination]
            if apply:
                argv += ["--apply", "--backup-dir", str(root / "backup")]
            with patch.object(split, "ROOT", root), patch.object(split, "BASE", base), \
                 patch.object(split.subprocess, "run", side_effect=command), patch("sys.argv", argv), patch("builtins.print"):

                split.main()
            for args in calls:
                self.assertIn(Path(args[args.index("-C") + 1]), (base, separation))
                self.assertNotIn("up", args)
            if apply:
                self.assertEqual((root / "backup/source-config.yaml").read_text(), "config: {}\n")
                self.assertEqual((root / "backup/destination-config.yaml").read_text(), "config: {}\n")
                state_moves = [args for args in calls if "move" in args]
                self.assertEqual(len(state_moves), 1)
                self.assertEqual(Path(state_moves[0][2]), base)
            else:
                self.assertFalse(any("set-all" in args or "move" in args for args in calls))

    def test_inspection_uses_layer_directories_without_mutations(self):
        self.exercise(False)

    def test_mocked_migration_uses_layer_paths_and_backs_up_configs(self):
        self.exercise(True)
