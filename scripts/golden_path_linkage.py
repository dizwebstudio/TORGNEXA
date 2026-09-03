"""Shared redacted linkage validation for the production golden path."""

from __future__ import annotations

import re
from typing import Any

from p4_common import QualificationError


FLOW_FIELDS = (
    "flow_ref",
    "order_ref",
    "reservation_ref",
    "shipment_ref",
    "return_ref",
    "refund_ref",
    "settlement_ref",
    "marking_ref",
    "edo_ref",
)
SAFE_LABEL = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._:/-]{0,191}$")


def fail(message: str) -> None:
    raise QualificationError(message)


def validate_flow(value: Any, name: str = "flow") -> None:
    """Validate the opaque, shared references carried by every v2 artifact."""
    if not isinstance(value, dict):
        fail(f"{name} must be an object")
    if set(value) != set(FLOW_FIELDS):
        fail(f"{name} must contain exactly the shared golden path references")
    for field in FLOW_FIELDS:
        reference = value[field]
        if not isinstance(reference, str) or SAFE_LABEL.fullmatch(reference) is None:
            fail(f"{name}.{field} must be a safe non-secret reference")


def same_flow(expected: Any, actual: Any, name: str) -> None:
    """Require an artifact to carry exactly the aggregate flow references."""
    validate_flow(actual, name)
    if actual != expected:
        fail(f"{name} does not match the aggregate golden path flow")
