#!/usr/bin/env python3
"""Move the legacy deployment object into native Pulumi stack configuration.

Run from the project root. Only configuration changes; no resources are deployed.
"""

import argparse
import json
from pathlib import Path
import subprocess
import tempfile

PROJECT = "LyricsPipeline"
LEGACY_DEPLOYMENT = f"{PROJECT}:deployment"
LEGACY_IMAGE = f"{PROJECT}:deploymentImage"


def plain_value(config: dict, key: str, default=None):
    entry = config.get(key)
    if entry is None:
        return default
    if entry.get("secret"):
        raise ValueError(f"{key} is encrypted; migrate it explicitly with Pulumi secret APIs")
    return entry.get("objectValue", entry.get("value", default))


def native_entry(value):
    if isinstance(value, dict):
        return {"value": json.dumps(value), "objectValue": value, "secret": False}
    return {"value": value, "secret": False}


def migration_values(config: dict, project: dict | None = None) -> dict:
    project = project or {}
    raw = plain_value(config, LEGACY_DEPLOYMENT, "{}")
    old = json.loads(raw) if isinstance(raw, str) else raw
    if not isinstance(old, dict):
        raise ValueError("legacy deployment must be an object")
    known = {
        "name", "region", "availabilityZone", "image", "taskCpu", "taskMemory",
        "minTasks", "maxTasks", "visibilitySeconds", "maxReceiveCount",
        "idleMinutes", "backlogPerWorker", "scaleOutCooldownSeconds", "tags",
    }
    unknown = old.keys() - known
    if unknown:
        raise ValueError(f"unknown legacy fields: {', '.join(sorted(unknown))}")

    # Frozen defaults from the old format, needed only for this one-time migration.
    region = old.get("region", "eu-central-1")
    zone = old.get("availabilityZone", "eu-central-1a")
    if not isinstance(region, str) or not region or not isinstance(zone, str) or not zone:
        raise ValueError("legacy region and availabilityZone must be non-empty strings")
    existing_region = plain_value(config, "aws:region")
    if existing_region is not None and existing_region != region:
        raise ValueError("legacy deployment.region disagrees with aws:region; resolve it first")

    def shared_object(key):
        value = plain_value(project, f"{PROJECT}:{key}", {})
        return dict(value)

    ecs = shared_object("ecs")
    for old_key, new_key in (("taskCpu", "cpuUnits"), ("taskMemory", "memoryMiB")):
        if old_key in old:
            value = old[old_key]
            if not isinstance(value, str) or not value.isdecimal():
                raise ValueError(f"legacy {old_key} must contain integer units")
            ecs[new_key] = int(value)
    image = plain_value(config, LEGACY_IMAGE, "") or old.get("image", "")
    if not isinstance(image, str):
        raise ValueError("legacy image must be a string")
    sqs = shared_object("sqs")
    sqs.update({key: old[key] for key in ("visibilitySeconds", "maxReceiveCount") if key in old})
    autoscaling = shared_object("autoscaling")
    autoscaling.update({key: old[key] for key in (
        "minTasks", "maxTasks", "idleMinutes", "backlogPerWorker", "scaleOutCooldownSeconds"
    ) if key in old})
    for key, value in (sqs | autoscaling).items():
        if type(value) is not int:
            raise ValueError(f"legacy {key} must be an integer")
    changes = {
        "aws:region": native_entry(region),
        f"{PROJECT}:vpc": native_entry(shared_object("vpc") | {"availabilityZone": zone}),
        f"{PROJECT}:image": native_entry(image),
        f"{PROJECT}:ecs": native_entry(ecs),
        f"{PROJECT}:sqs": native_entry(sqs),
        f"{PROJECT}:autoscaling": native_entry(autoscaling),
    }
    if "name" in old:
        if not isinstance(old["name"], str) or not old["name"].strip():
            raise ValueError("legacy name must be a non-empty string")
        changes[f"{PROJECT}:name"] = native_entry(old["name"])
    if "tags" in old:
        tags = old["tags"]
        if not isinstance(tags, dict) or any(not isinstance(v, str) for v in tags.values()):
            raise ValueError("legacy tags must be an object of strings")
        changes[f"{PROJECT}:tags"] = native_entry(shared_object("tags") | tags)
    for key, entry in changes.items():
        current = plain_value(config, key)
        shared = plain_value(project, key)
        if key in config and current != shared and current != plain_value(changes, key):
            raise ValueError(f"native setting {key} already differs; refusing to overwrite it")
    # Keep only deliberate differences in the stack, never copies of project values.
    return {key: entry for key, entry in changes.items()
            if plain_value(changes, key) != plain_value(project, key)}


def migrate(stack: str, config_file: Path) -> bool:
    def pulumi(*args, settings=config_file):
        options = ["--stack", stack, "--config-file", str(settings), "--non-interactive"]
        return subprocess.run(["pulumi", *args, *options], check=True, text=True,
                              capture_output=True).stdout

    current = json.loads(pulumi("config", "--json"))
    if LEGACY_DEPLOYMENT not in current and LEGACY_IMAGE not in current:
        return False
    # Ask Pulumi for project-only values using an empty temporary stack file.
    # This honors native configuration syntax without adding a YAML parser.
    with tempfile.TemporaryDirectory() as directory:
        empty = Path(directory) / "project-values.yaml"
        empty.write_text("config: {}\n")
        project = json.loads(pulumi("config", "--json", settings=empty))
    if any(f"{PROJECT}:{key}" not in project for key in ("vpc", "s3", "ecs", "sqs", "autoscaling")):
        raise ValueError("configure shared settings in Pulumi.yaml before migrating")
    changes = migration_values(current, project)
    managed = {"aws:region", *(f"{PROJECT}:{key}" for key in (
        "name", "tags", "image", "vpc", "ecs", "sqs", "autoscaling"
    ))}
    redundant = managed - changes.keys()
    for key in redundant:
        if key in current and plain_value(current, key) != plain_value(project, key):
            raise ValueError(f"native setting {key} is not covered by this migration; refusing to remove it")
    original = config_file.read_bytes()
    try:
        if changes:
            pulumi("config", "set-all", "--json", json.dumps(changes))
        # Remove redundant stack values as well as legacy keys. Pulumi config rm
        # changes only the stack file; inherited project settings stay intact.
        for key in sorted(redundant):
            pulumi("config", "rm", key)
        for key in (LEGACY_IMAGE, LEGACY_DEPLOYMENT):
            if key in current:
                pulumi("config", "rm", key)
    except (OSError, subprocess.CalledProcessError):
        config_file.write_bytes(original)
        raise
    return True


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--stack", required=True, help="Existing stack name, optionally org/project/stack")
    parser.add_argument("--config-file", type=Path, help="Defaults to Pulumi.<stack>.yaml")
    args = parser.parse_args()
    config_file = args.config_file or Path(f"Pulumi.{args.stack.rsplit('/', 1)[-1]}.yaml")
    try:
        changed = migrate(args.stack, config_file)
    except subprocess.CalledProcessError as error:
        parser.exit(1, error.stderr or "Pulumi configuration migration failed\n")
    except (OSError, ValueError) as error:
        parser.exit(1, f"{error}\n")
    print(f"Migrated {config_file}; run pulumi preview for the same stack." if changed
          else "No legacy configuration found; nothing changed.")


if __name__ == "__main__":
    main()
