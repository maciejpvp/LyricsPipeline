from pathlib import Path
import time


HEARTBEAT = Path("/tmp/vocal-extractor/health")
MAX_AGE_SECONDS = 180


try:
    age = time.time() - HEARTBEAT.stat().st_mtime
except FileNotFoundError:
    raise SystemExit(1)

raise SystemExit(0 if 0 <= age <= MAX_AGE_SECONDS else 1)
