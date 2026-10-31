import sys
from pathlib import Path
from types import ModuleType

import pytest

sys.path.insert(0, str(Path(__file__).parents[1]))

from audio import AudioProcessor


@pytest.fixture
def separator_stub(monkeypatch):
    calls = {}

    class FakeSeparator:
        def __init__(self, **kwargs):
            calls["settings"] = kwargs

        def load_model(self, **kwargs):
            calls["model"] = kwargs

        def separate(self, input_path, **kwargs):
            calls["input"] = input_path
            calls["separate"] = kwargs
            return calls["outputs"]

    package = ModuleType("audio_separator")
    module = ModuleType("audio_separator.separator")
    module.Separator = FakeSeparator
    package.separator = module
    monkeypatch.setitem(sys.modules, "audio_separator", package)
    monkeypatch.setitem(sys.modules, "audio_separator.separator", module)
    return calls


@pytest.mark.parametrize("absolute", [False, True])
def test_separate_requests_only_vocals_with_fixed_name(tmp_path, separator_stub, absolute):
    output_dir = tmp_path / "output"
    output_dir.mkdir()
    vocals = output_dir / "vocals.wav"
    vocals.write_bytes(b"vocals")
    separator_stub["outputs"] = [str(vocals) if absolute else vocals.name]
    processor = AudioProcessor(tmp_path / "models", "model.onnx", 30)

    assert processor.separate(tmp_path / "song.mp3", output_dir) == vocals.resolve()
    assert separator_stub["settings"] == {
        "output_dir": str(output_dir), "output_format": "WAV",
        "model_file_dir": str(tmp_path / "models"), "output_single_stem": "Vocals",
    }
    assert separator_stub["model"] == {"model_filename": "model.onnx"}
    assert separator_stub["input"] == str(tmp_path / "song.mp3")
    assert separator_stub["separate"] == {"custom_output_names": {"Vocals": "vocals"}}


@pytest.mark.parametrize("outputs", [[], ["vocals.wav", "instrumental.wav"]])
def test_separate_rejects_missing_or_multiple_stems(tmp_path, separator_stub, outputs):
    separator_stub["outputs"] = outputs
    with pytest.raises(RuntimeError, match="exactly one vocal stem"):
        AudioProcessor(tmp_path, "model.onnx", 30).separate(tmp_path / "song.wav", tmp_path)


@pytest.mark.parametrize("name", ["instrumental.wav", "song_vocals_(Instrumental).wav", "Vocals.wav"])
def test_separate_rejects_unexpected_filename(tmp_path, separator_stub, name):
    (tmp_path / name).write_bytes(b"audio")
    separator_stub["outputs"] = [name]
    with pytest.raises(RuntimeError, match="did not produce vocals.wav"):
        AudioProcessor(tmp_path, "model.onnx", 30).separate(tmp_path / "song.wav", tmp_path)


@pytest.mark.parametrize("case", ["missing", "directory", "outside", "symlink"])
def test_separate_rejects_invalid_output_path(tmp_path, separator_stub, case):
    output_dir = tmp_path / "output"
    output_dir.mkdir()
    outside = tmp_path / "vocals.wav"
    outside.write_bytes(b"audio")
    output = output_dir / "vocals.wav"
    if case == "directory":
        output.mkdir()
    elif case == "outside":
        output = Path("..") / "vocals.wav"
    elif case == "symlink":
        output.symlink_to(outside)
    separator_stub["outputs"] = [str(output)]
    with pytest.raises(RuntimeError, match="unsafe output path"):
        AudioProcessor(tmp_path, "model.onnx", 30).separate(tmp_path / "song.wav", output_dir)
