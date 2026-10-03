from __future__ import annotations

import subprocess
from pathlib import Path

SUPPORTED_EXTENSIONS = {".mp3", ".wav", ".flac", ".m4a", ".ogg", ".aac", ".wma"}


class AudioProcessor:
    def __init__(self, model_dir: Path, model_filename: str, ffprobe_timeout_seconds: int):
        self.model_dir = model_dir
        self.model_filename = model_filename
        self.ffprobe_timeout_seconds = ffprobe_timeout_seconds

    def validate_name(self, name: str) -> str:
        normalized = Path(name).name
        if normalized != name or Path(normalized).suffix.lower() not in SUPPORTED_EXTENSIONS:
            raise ValueError("unsupported audio file type")
        return normalized

    def validate_audio(self, path: Path) -> None:
        result = subprocess.run(
            ["ffprobe", "-v", "error", "-show_entries", "format=format_name", "-of", "json", str(path)],
            capture_output=True,
            text=True,
            timeout=self.ffprobe_timeout_seconds,
            check=False,
        )
        if result.returncode != 0 or not result.stdout.strip():
            raise ValueError("invalid_audio")

    def separate(self, input_path: Path, output_dir: Path) -> list[Path]:
        from audio_separator.separator import Separator

        separator = Separator(output_dir=str(output_dir), output_format="WAV", model_file_dir=str(self.model_dir))
        separator.load_model(model_filename=self.model_filename)
        output_root = output_dir.resolve()
        outputs: list[Path] = []
        for path in separator.separate(str(input_path)):
            candidate = Path(path) if Path(path).is_absolute() else output_dir / path
            resolved = candidate.resolve()
            if resolved.parent != output_root or not resolved.is_file():
                raise RuntimeError("separator produced an unsafe output path")
            outputs.append(resolved)
        if not any("vocal" in path.name.lower() for path in outputs):
            raise RuntimeError("separator did not produce a vocal stem")
        return outputs
