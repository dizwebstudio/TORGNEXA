import importlib.util
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock


SPEC = importlib.util.spec_from_file_location("runtime_load", Path(__file__).with_name("runtime-load.py"))
runtime_load = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(runtime_load)


class FakeResponse:
    def __init__(self, content_type, body):
        self.status = 200
        self.headers = {"Content-Type": content_type}
        self.body = body
        self.lines = iter(body.splitlines(keepends=True))

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self, _limit):
        return self.body

    def readline(self, _limit):
        return next(self.lines, b"")


def urlopen(request, timeout):
    del timeout
    if request.full_url.endswith("/sse"):
        return FakeResponse("text/event-stream", b"event: ready\ndata: {}\n\n")
    return FakeResponse("application/json", b'{"status":"ok"}')


class RuntimeLoadTest(unittest.TestCase):
    def test_report_write_is_private_and_refuses_replacement(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "report.json"
            runtime_load.write_report(str(output), '{"redacted":true}\n')
            self.assertEqual(os.stat(output).st_mode & 0o777, 0o600)
            with self.assertRaises(FileExistsError):
                runtime_load.write_report(str(output), '{}\n')
            self.assertEqual(output.read_text(encoding="utf-8"), '{"redacted":true}\n')

    def test_authenticated_mix_and_concurrent_sse_are_redacted(self):
        token = "synthetic-header.synthetic-payload.synthetic-signature"
        with mock.patch.object(runtime_load.urllib.request, "urlopen", side_effect=urlopen):
            with tempfile.TemporaryDirectory() as directory:
                token_file = Path(directory) / "token"
                token_file.write_text(token, encoding="ascii")
                os.chmod(token_file, 0o600)
                args = type(
                    "Args",
                    (),
                    {
                        "base_url": "http://127.0.0.1:18080",
                        "path": ["/one", "/two"],
                        "token_file": str(token_file),
                        "sse_path": "/sse",
                        "sse_clients": 4,
                        "sse_timeout": 3,
                        "sse_ready_timeout": 2,
                        "requests": 100,
                        "concurrency": 8,
                        "timeout": 2,
                        "availability_min": 1,
                        "p99_max_ms": 2000,
                        "throughput_min": 1,
                        "sse_availability_min": 1,
                        "sse_p99_max_ms": 3000,
                    },
                )()
                report = runtime_load.run_authenticated(args)
                self.assertTrue(report["passed"])
                self.assertEqual(report["sse"]["peak_connected"], 4)
                self.assertEqual(len(report["http"]["routes"]), 2)
                self.assertNotIn(token, json.dumps(report))


if __name__ == "__main__":
    unittest.main()
