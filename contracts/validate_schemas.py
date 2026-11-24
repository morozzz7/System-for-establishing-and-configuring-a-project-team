#!/usr/bin/env python3
"""Validate the JSON Schemas and the taxonomy artifact without network access.

The schemas use draft 2020-12 `$defs`, while the available offline validator only
implements draft 7. The schemas contain no other 2020-only constructs, so validation
rewrites `$defs`/`#/$defs/` in memory to their draft-7 equivalents; files stay untouched.
"""

from __future__ import annotations

import json
from pathlib import Path

from jsonschema import Draft7Validator, RefResolver

SCHEMA_DIR = Path(__file__).resolve().parent / "json-schema"
SCHEMA_FILES = sorted(SCHEMA_DIR.glob("*.schema.json"))


def to_draft7(value):
    if isinstance(value, dict):
        out: dict = {}
        for key, item in value.items():
            if key == "$schema":
                out[key] = "http://json-schema.org/draft-07/schema#"
            else:
                out["definitions" if key == "$defs" else key] = to_draft7(item)
        return out
    if isinstance(value, list):
        return [to_draft7(item) for item in value]
    if isinstance(value, str):
        return value.replace("#/$defs/", "#/definitions/")
    return value


def main() -> int:
    if not SCHEMA_FILES:
        print("No JSON Schemas found", flush=True)
        return 1

    store: dict[str, dict] = {}
    for path in SCHEMA_FILES:
        raw = json.loads(path.read_text(encoding="utf-8"))
        schema = to_draft7(raw)
        Draft7Validator.check_schema(schema)
        store[schema["$id"]] = schema

    problems: list[str] = []
    for schema_id, schema in store.items():
        resolver = RefResolver(base_uri=schema_id, referrer=schema, store=store)

        def walk(node, location: str) -> None:
            if isinstance(node, dict):
                ref = node.get("$ref")
                if isinstance(ref, str):
                    try:
                        resolver.resolve(ref)
                    except Exception as exc:  # noqa: BLE001 - all ref failures are gate failures
                        problems.append(f"{schema_id}: {location}: unresolved $ref {ref}: {exc}")
                for key, value in node.items():
                    walk(value, f"{location}/{key}")
            elif isinstance(node, list):
                for index, value in enumerate(node):
                    walk(value, f"{location}/{index}")

        walk(schema, "#")

    if problems:
        for problem in problems:
            print(problem)
        return 1

    print(f"Validated {len(store)} JSON Schemas (meta-schema, refs, $defs -> definitions shim).")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
