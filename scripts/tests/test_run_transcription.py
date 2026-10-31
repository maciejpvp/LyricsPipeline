import importlib.util
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("runner", Path(__file__).parents[1] / "run-transcription.py")
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)


class ManualRunnerTests(unittest.TestCase):
    def test_starts_only_workflow_for_existing_object(self):
        values = {"region": "eu-central-1", "accountId": "123", "mediaBucket": "media",
                  "outputPrefix": "output", "stateMachineArn": "workflow"}
        clients = {name: Mock() for name in ("sts", "s3", "stepfunctions")}
        clients["sts"].get_caller_identity.return_value = {"Account": "123"}
        clients["s3"].head_object.return_value = {"ETag": '"source"'}
        clients["stepfunctions"].start_execution.return_value = {"executionArn": "execution"}
        session = Mock()
        session.client.side_effect = clients.__getitem__
        with patch.object(runner.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(values))), \
             patch.object(runner.boto3, "Session", return_value=session), \
             patch("sys.argv", ["runner", "--stack", "org/LyricsPipeline-Transcription/dev", "--s3-key", "output/song/vocals.wav"]), \
             patch("builtins.print"):
            runner.main()
        request = clients["stepfunctions"].start_execution.call_args.kwargs
        self.assertEqual(request["stateMachineArn"], "workflow")
        self.assertEqual(json.loads(request["input"]), {"bucket": "media", "key": "output/song/vocals.wav", "etag": "source", "attempt": "default"})
        clients["s3"].head_object.assert_called_once_with(Bucket="media", Key="output/song/vocals.wav")
        self.assertEqual([call.args[0] for call in session.client.call_args_list], ["sts", "s3", "stepfunctions"])

    def test_wrong_account_never_starts_execution(self):
        values = {"region": "eu-central-1", "accountId": "expected"}
        session = Mock()
        session.client.return_value.get_caller_identity.return_value = {"Account": "other"}
        with patch.object(runner.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, json.dumps(values))), \
             patch.object(runner.boto3, "Session", return_value=session), \
             patch("sys.argv", ["runner", "--stack", "stack", "--s3-key", "output/song/vocals.wav"]):
            with self.assertRaisesRegex(ValueError, "different account"):
                runner.main()
        self.assertEqual([call.args[0] for call in session.client.call_args_list], ["sts"])
