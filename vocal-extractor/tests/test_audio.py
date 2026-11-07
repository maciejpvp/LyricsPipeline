import sys
from pathlib import Path
from types import ModuleType, SimpleNamespace

import pytest

sys.path.insert(0, str(Path(__file__).parents[1]))

from audio import AudioProcessor


@pytest.fixture
def separator_stub(monkeypatch):
    calls = {"instances": [], "model_loads": 0, "jobs": []}

    class FakeSeparator:
        def __init__(self, **kwargs):
            calls["settings"] = kwargs
            calls["instances"].append(self)
            self.output_dir = kwargs["output_dir"]
            self.model_instance = None

        def load_model(self, **kwargs):
            calls["model"] = kwargs
            calls["model_loads"] += 1
            if calls.get("load_error"):
                raise calls["load_error"]
            self.model_instance = SimpleNamespace(output_dir=self.output_dir)

        def separate(self, input_path, **kwargs):
            calls["input"] = input_path
            calls["separate"] = kwargs
            calls["jobs"].append((input_path, self.output_dir, self.model_instance.output_dir))
            if calls.get("separation_error"):
                raise calls["separation_error"]
            if calls.get("write_output"):
                output = Path(self.model_instance.output_dir) / "vocals.wav"
                output.write_bytes(Path(input_path).read_bytes())
                return [str(Path(self.output_dir) / output.name)]
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


def test_model_load_is_lazy(tmp_path, separator_stub):
    processor = AudioProcessor(tmp_path, "model.onnx", 30)

    processor.validate_name("song.wav")

    assert separator_stub["instances"] == []
    assert separator_stub["model_loads"] == 0


def test_model_is_reused_for_jobs_with_different_output_directories(tmp_path, separator_stub):
    separator_stub["write_output"] = True
    processor = AudioProcessor(tmp_path / "models", "model.onnx", 30)

    for job in ("first", "second"):
        source = tmp_path / f"{job}.wav"
        source.write_bytes(job.encode())
        output_dir = tmp_path / job
        output_dir.mkdir()

        output = processor.separate(source, output_dir)

        assert output == (output_dir / "vocals.wav").resolve()
        assert output.read_bytes() == job.encode()
        assert list(output_dir.iterdir()) == [output]

    assert len(separator_stub["instances"]) == 1
    assert separator_stub["model_loads"] == 1
    assert separator_stub["jobs"] == [
        (str(tmp_path / f"{job}.wav"), str(tmp_path / job), str(tmp_path / job))
        for job in ("first", "second")
    ]


def test_failed_model_load_is_retried(tmp_path, separator_stub):
    separator_stub["load_error"] = RuntimeError("model load failed")
    separator_stub["write_output"] = True
    source = tmp_path / "song.wav"
    source.write_bytes(b"audio")
    processor = AudioProcessor(tmp_path, "model.onnx", 30)

    with pytest.raises(RuntimeError, match="model load failed"):
        processor.separate(source, tmp_path)

    del separator_stub["load_error"]
    assert processor.separate(source, tmp_path).read_bytes() == b"audio"
    assert len(separator_stub["instances"]) == 2
    assert separator_stub["model_loads"] == 2


def test_separation_failure_does_not_reload_model_for_next_job(tmp_path, separator_stub):
    separator_stub["separation_error"] = RuntimeError("separation failed")
    separator_stub["write_output"] = True
    source = tmp_path / "song.wav"
    source.write_bytes(b"audio")
    processor = AudioProcessor(tmp_path, "model.onnx", 30)

    with pytest.raises(RuntimeError, match="separation failed"):
        processor.separate(source, tmp_path)

    del separator_stub["separation_error"]
    output_dir = tmp_path / "retry"
    output_dir.mkdir()
    assert processor.separate(source, output_dir).read_bytes() == b"audio"
    assert len(separator_stub["instances"]) == 1
    assert separator_stub["model_loads"] == 1


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
