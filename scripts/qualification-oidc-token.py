#!/usr/bin/env python3
"""Acquire one disposable qualification JWT without printing credential material."""

import argparse
import json
import os
import re
import stat
import sys
import urllib.parse
import urllib.request


SAFE_IDENTIFIER = re.compile(r"^[A-Za-z0-9._@+-]{1,128}$")


def acquire(endpoint, client_id, username, password, timeout=10):
    parsed = urllib.parse.urlsplit(endpoint)
    if parsed.scheme not in {"http", "https"} or not parsed.hostname or parsed.username or parsed.password or parsed.fragment:
        raise ValueError("invalid token endpoint")
    if not SAFE_IDENTIFIER.fullmatch(client_id) or not SAFE_IDENTIFIER.fullmatch(username) or not password or len(password) > 1024:
        raise ValueError("invalid bounded qualification identity")
    body = urllib.parse.urlencode(
        {"grant_type": "password", "client_id": client_id, "username": username, "password": password}
    ).encode("ascii")
    request = urllib.request.Request(
        endpoint,
        data=body,
        method="POST",
        headers={"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json", "User-Agent": "torgnexa-qualification/1"},
    )
    with urllib.request.urlopen(request, timeout=timeout) as response:
        raw = response.read(65_537)
        if response.status != 200 or len(raw) > 65_536:
            raise ValueError("qualification token endpoint rejected the request")
    payload = json.loads(raw)
    token = payload.get("access_token")
    if not isinstance(token, str) or len(token) < 32 or len(token) > 32_768 or token.count(".") != 2 or token.strip() != token:
        raise ValueError("qualification token endpoint returned an invalid JWT")
    return token


def private_write(path, token):
    if os.path.lexists(path):
        raise ValueError("refusing to replace an existing token path")
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(descriptor, "w", encoding="ascii") as handle:
            handle.write(token)
        if stat.S_IMODE(os.stat(path).st_mode) != 0o600:
            raise ValueError("token file permissions are not private")
    except Exception:
        try:
            os.unlink(path)
        except FileNotFoundError:
            pass
        raise


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--client-id", required=True)
    parser.add_argument("--username", required=True)
    parser.add_argument("--output", required=True)
    args = parser.parse_args()
    password = os.environ.get("TORGNEXA_QUALIFICATION_OIDC_PASSWORD", "")
    try:
        private_write(args.output, acquire(args.endpoint, args.client_id, args.username, password))
    except (OSError, ValueError, json.JSONDecodeError) as error:
        print(f"qualification-oidc-token: {error}", file=sys.stderr)
        raise SystemExit(1)


if __name__ == "__main__":
    main()
