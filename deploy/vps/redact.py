"""Fail-closed filtering for isolated CI migration diagnostics; never a raw-log fallback."""
import argparse
import os
from pathlib import Path
import re
import shlex
import sys
from urllib.parse import quote, quote_plus

MAX_INPUT = 1024 * 1024
SUPPRESSED = "Migration diagnostics suppressed: safe redaction unavailable."
_SECRET_NAME = re.compile(r"(?:^PGPASSWORD$|(?:^|_)(?:PASSWORD|PASS|SECRET|TOKEN|KEY|KEK)(?:_|$))", re.I)
_ENV_LINE = re.compile(r"^([A-Za-z_][A-Za-z0-9_]*)=(.*)$")
_ANSI = re.compile(r"\x1b\[[0-?]*[ -/]*[@-~]")
_URI_USER = re.compile(r"\b([a-z][a-z0-9+.-]*://)[^/\s]+@", re.I)
_QUERY = re.compile(r"(https?://[^\s?]+)\?[^\s]+", re.I)
_ASSIGNMENT = re.compile(r'''(?m)\b([A-Za-z_][A-Za-z0-9_]*)(\s*[:=]\s*)("[^"]*"|'[^']*'|[^\s,;]+)''')
_SQL_PASSWORD = re.compile(r'''(?i)(\b(?:PASSWORD|PGPASSWORD)\b\s*(?:=\s*)?)(?:'(?:''|[^'])*'|"[^"]*"|[^\s,;]+)''')
_PRIVATE_KEY = re.compile(r"-----BEGIN (?:[A-Z]+ )?PRIVATE KEY-----.*?-----END (?:[A-Z]+ )?PRIVATE KEY-----", re.S)


def secret_values(env_file, environ):
    values = []
    for line in Path(env_file).read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        match = _ENV_LINE.fullmatch(line)
        if not match:
            raise ValueError("unsupported environment syntax")
        name, raw = match.groups()
        raw = raw.strip()
        if raw.startswith(("'", '"')):
            tokens = shlex.split(raw, comments=False, posix=True)
            if len(tokens) != 1:
                raise ValueError("unsupported environment syntax")
            value = tokens[0]
        else:
            if any(c.isspace() for c in raw):
                raise ValueError("unsupported environment syntax")
            value = raw
        if _SECRET_NAME.search(name) and value:
            # Generated smoke secrets are literal. Unknown shell expansion must
            # suppress diagnostics rather than guess the effective credential.
            if not raw.startswith("'") and ("$" in raw or "`" in raw):
                raise ValueError("unsupported secret expansion")
            values.append(value)
    values.extend(value for name, value in environ.items() if value and _SECRET_NAME.search(name))
    return values


def redact(text, values):
    text = _ANSI.sub("", text)
    variants = set()
    for value in values:
        variants.update((value, quote(value, safe=""), quote_plus(value), value.replace("'", "''")))
    for value in sorted(variants, key=len, reverse=True):
        if value:
            text = text.replace(value, "[REDACTED]")
    text = _PRIVATE_KEY.sub("[PRIVATE KEY REDACTED]", text)
    if "-----BEGIN" in text or "-----END" in text:
        raise ValueError("incomplete key material")
    text = _URI_USER.sub(r"\1[REDACTED]@", text)
    text = _QUERY.sub(r"\1?[QUERY REDACTED]", text)
    text = _ASSIGNMENT.sub(lambda m: m[1] + m[2] + "[REDACTED]" if _SECRET_NAME.search(m[1]) else m[0], text)
    return _SQL_PASSWORD.sub(r"\1[REDACTED]", text)


def main(argv=None):
    parser = argparse.ArgumentParser()
    parser.add_argument("--env-file", required=True)
    args = parser.parse_args(argv)
    try:
        values = secret_values(args.env_file, os.environ)
        raw = sys.stdin.buffer.read(MAX_INPUT + 1)
        if len(raw) > MAX_INPUT:
            raise ValueError("diagnostic output too large")
        safe = redact(raw.decode("utf-8"), values)
    except Exception:
        # No exception text, file contents, traceback, or original input.
        print(SUPPRESSED, file=sys.stderr)
        return 2
    print(safe, end="")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())