import os
from pathlib import Path
import subprocess
import tempfile
import unittest


class RestorationAckTests(unittest.TestCase):
    def test_retries_transient_errors_and_lost_ack_without_exposing_token(self):
        script = Path(__file__).resolve().parents[1] / "scripts/gomi-ack-restoration"
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / "curl").write_text("""#!/bin/sh
printf '%s\\n' "$*" >> "$ACK_TEST_ROOT/requests"
count=$(wc -l < "$ACK_TEST_ROOT/requests")
# Connection failure, then lost acknowledgement, then a successful retry.
[ "$count" -ge 3 ]
""")
            (root / "sleep").write_text("#!/bin/sh\nexit 0\n")
            for name in ("curl", "sleep"):
                (root / name).chmod(0o755)
            result = subprocess.run(["bash", str(script), "http://gomi/events?token=private-token&attempt_id=current"],
                                    env={**os.environ, "PATH": str(root) + ":" + os.environ["PATH"], "ACK_TEST_ROOT": str(root)},
                                    capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            requests = (root / "requests").read_text().splitlines()
            self.assertEqual(len(requests), 3)
            self.assertEqual(len(set(requests)), 1)
            self.assertIn("type=image_applied", requests[0])
            self.assertNotIn("private-token", result.stdout + result.stderr)

    def test_missing_endpoint_fails_without_acknowledging(self):
        script = Path(__file__).resolve().parents[1] / "scripts/gomi-ack-restoration"
        result = subprocess.run(["bash", str(script), ""], capture_output=True)
        self.assertNotEqual(result.returncode, 0)
