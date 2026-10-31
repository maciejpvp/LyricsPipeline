#!/usr/bin/env python3
"""Inspect a monolithic stack, or move separation resources/config with --apply.

Never deploys, deletes or recreates cloud resources. Initialize the empty destination
stack first. Stop concurrent Pulumi updates for both stacks during this operation.
"""
import argparse
from datetime import datetime, timezone
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[1]
BASE = ROOT / "infra/base"
SEPARATION_TYPES = {"lyrics:network:Network", "lyrics:registry:Registry", "lyrics:iam:WorkerIAM",
                    "lyrics:image:Image", "lyrics:worker:Worker", "lyrics:autoscaling:Autoscaling"}
KEYS = ("name", "tags", "vpc", "ecs", "autoscaling", "image")


def move_roots(document):
    resources = document["deployment"]["resources"]
    roots = [r["urn"] for r in resources if r["type"] in SEPARATION_TYPES]
    found = {r["type"] for r in resources if r["type"] in SEPARATION_TYPES}
    if not roots:
        raise ValueError("source has no managed separation resources; identify their real owner before migration")
    if found != SEPARATION_TYPES or len(roots) != len(SEPARATION_TYPES):
        raise ValueError("source contains an incomplete or ambiguous separation graph; inspect state manually")
    return roots


def config_changes(current, source, destination):
    if any(key in current for key in ("LyricsPipeline:deployment", "LyricsPipeline:deploymentImage")):
        raise ValueError("legacy deployment configuration: migrate to native configuration before splitting stacks")
    changes = {"LyricsPipeline-Separation:baseStack": {"value": source, "secret": False}}
    for key in ("aws:region", *(f"LyricsPipeline:{key}" for key in KEYS)):
        if key in current:
            entry = current[key]
            if entry.get("secret"):
                raise ValueError(f"{key} is secret; transfer it with Pulumi secret APIs before migration")
            changes[key.replace("LyricsPipeline:", "LyricsPipeline-Separation:", 1)] = entry
    return changes


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, help="org/LyricsPipeline/environment")
    parser.add_argument("--dest", required=True, help="org/LyricsPipeline-Separation/environment")
    parser.add_argument("--apply", action="store_true", help="Back up states/config and perform the state move")
    parser.add_argument("--backup-dir", type=Path)
    args = parser.parse_args()
    for value, project in ((args.source, "LyricsPipeline"), (args.dest, "LyricsPipeline-Separation")):
        if len(value.split("/")) != 3 or value.split("/")[1] != project:
            parser.error(f"expected fully qualified org/{project}/environment")

    def cli(folder, *command):
        return subprocess.run(["pulumi", "-C", str(folder), *command, "--non-interactive"],
                              check=True, capture_output=True, text=True).stdout

    try:
        source = cli(BASE, "stack", "export", "--stack", args.source)
        dest = cli(ROOT / "infra/separation", "stack", "export", "--stack", args.dest)
        destination = json.loads(dest)["deployment"]["resources"]
        if any(r["type"] != "pulumi:pulumi:Stack" for r in destination):
            raise ValueError("destination is not empty; refusing to merge independently managed infrastructure")
        roots = move_roots(json.loads(source))
        current = json.loads(cli(BASE, "config", "--json", "--stack", args.source))
        changes = config_changes(current, args.source, args.dest)
        print("Move these components and all their children:")
        print("\n".join(roots))
        if not args.apply:
            print("Read-only inspection complete. Use --apply after reviewing the ownership and stopping concurrent updates.")
            return
        folder = args.backup_dir or Path("/tmp") / ("lyrics-stack-split-" + datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%S%fZ"))
        folder.mkdir(mode=0o700, parents=True, exist_ok=False)
        os.chmod(folder, 0o700)
        for name, text in (("source-state.json", source), ("destination-state.json", dest)):
            path = folder / name
            path.write_text(text)
            path.chmod(0o600)
        for label, project, stack in (("source", BASE, args.source), ("destination", ROOT / "infra/separation", args.dest)):
            original = project / f"Pulumi.{stack.rsplit('/', 1)[-1]}.yaml"
            if original.exists():
                backup = folder / f"{label}-config.yaml"
                backup.write_bytes(original.read_bytes())
                backup.chmod(0o600)
        print(f"Backups: {folder}")
        cli(ROOT / "infra/separation", "config", "set-all", "--json", json.dumps(changes), "--stack", args.dest)
        cli(BASE, "state", "move", "--source", args.source, "--dest", args.dest, "--yes", *roots)
        for key in ("vpc", "ecs", "autoscaling", "image"):
            if f"LyricsPipeline:{key}" in current:
                cli(BASE, "config", "rm", f"LyricsPipeline:{key}", "--stack", args.source)
        print("State moved. Preview the base, publish its outputs, then preview separation. No pulumi up was run.")
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f"{getattr(error, 'stderr', '') or error}\nNo automatic state rollback was attempted. Inspect backups if --apply was used.\n")


if __name__ == "__main__":
    main()
