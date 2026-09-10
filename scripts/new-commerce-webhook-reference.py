#!/usr/bin/env python3
"""Provision a random callback reference; persist only the configuration digest."""
import argparse
import hashlib
import json
import secrets

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("topic", choices=[f"{resource}.{action}" for resource in
                    ("order", "product", "coupon", "customer") for action in
                    ("created", "updated", "deleted")])
args = parser.parse_args()
reference = secrets.token_urlsafe(32)
print(json.dumps({
    "callback_query": "subscription=" + reference,
    "configuration_binding": {
        "reference_sha256": hashlib.sha256(reference.encode("ascii")).hexdigest(),
        "topic": args.topic,
    },
}, indent=2))
