import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.path.insert(0, str(Path(__file__).parent))
from redact import MAX_INPUT, SUPPRESSED, redact, secret_values


class MigrationRedactionTest(unittest.TestCase):
    def test_known_raw_sql_and_url_encoded_secrets_preserve_diagnosis(self):
        secret = "private marker+'/value"
        raw = "SQLSTATE 42501 migration 20261024022325 at line 4\n" + secret + "\n" + secret.replace("'", "''") + "\nprivate%20marker%2B%27%2Fvalue"
        safe = redact(raw, [secret])
        self.assertIn("SQLSTATE 42501 migration 20261024022325 at line 4", safe)
        self.assertNotIn("private", safe)
        self.assertEqual(safe.count("[REDACTED]"), 3)

    def test_generic_credentials_queries_and_private_keys_are_removed(self):
        raw = "SQLSTATE 42501\npostgres://operator:unknown-password@db:5432/pf\nCREATE ROLE x PASSWORD 'unknown-sql-password';\nAG_SMTP_PASSWORD=unknown-env-password\nhttps://files.example.test/x?signature=unknown-signature\n-----BEGIN PRIVATE KEY-----\nunknown-pem-key\n-----END PRIVATE KEY-----\n"
        safe = redact(raw, [])
        self.assertIn("SQLSTATE 42501", safe)
        self.assertNotIn("unknown-", safe)

    def test_environment_and_quoted_file_values_are_redacted(self):
        with tempfile.TemporaryDirectory() as tmp:
            env_file = Path(tmp) / ".env"
            env_file.write_text("# generated\nAG_DOMAIN=vps.example.test\nAG_DB_APP_PASSWORD='private value $literal'\n", encoding="utf-8")
            values = secret_values(env_file, {"S3_APP_SECRET_KEY": "environment-marker", "PGPASSWORD": "pg-environment-marker"})
            safe = redact("SQLSTATE 42501 private value $literal environment-marker pg-environment-marker", values)
            self.assertEqual(safe, "SQLSTATE 42501 [REDACTED] [REDACTED] [REDACTED]")

    def test_ansi_cannot_split_a_known_secret(self):
        self.assertEqual(redact("SQLSTATE 42501 private\x1b[31m-marker", ["private-marker"]), "SQLSTATE 42501 [REDACTED]")

    def run_cli(self, env_file, raw):
        child_env = {key: value for key, value in os.environ.items() if key.upper() in {"SYSTEMROOT", "WINDIR", "PATH", "TEMP", "TMP"}}
        return subprocess.run([sys.executable, "-X", "utf8", str(Path(__file__).with_name("redact.py")), "--env-file", str(env_file)],
                              input=raw, text=True, encoding="utf-8", capture_output=True, check=False, env=child_env)

    def assert_suppressed(self, result):
        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertEqual(result.stderr.strip(), SUPPRESSED)

    def test_missing_env_never_falls_back_to_raw(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assert_suppressed(self.run_cli(Path(tmp) / "absent.env", "private-input-marker SQLSTATE 42501"))

    def test_unknown_shell_expansion_never_guesses_secret(self):
        with tempfile.TemporaryDirectory() as tmp:
            env_file = Path(tmp) / ".env"
            env_file.write_text("AG_DB_APP_PASSWORD=$UNKNOWN_SECRET\n", encoding="utf-8")
            self.assert_suppressed(self.run_cli(env_file, "private-input-marker SQLSTATE 42501"))

    def test_oversized_input_is_not_partially_printed(self):
        with tempfile.TemporaryDirectory() as tmp:
            env_file = Path(tmp) / ".env"
            env_file.write_text("AG_DOMAIN=vps.example.test\n", encoding="utf-8")
            self.assert_suppressed(self.run_cli(env_file, "x" * (MAX_INPUT + 1)))

    def test_incomplete_key_is_never_partially_printed(self):
        with self.assertRaises(ValueError):
            redact("SQLSTATE 42501\n-----BEGIN PRIVATE KEY-----\nprivate-input-marker", [])


if __name__ == "__main__":
    unittest.main()