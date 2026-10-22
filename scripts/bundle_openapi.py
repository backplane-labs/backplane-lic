#!/usr/bin/env python3
"""Bundle the OpenAPI 3.1 fragments under openapi/src into one document.

openapi/src/
  openapi.yaml     the document skeleton (info, servers, tags)
  components.yaml  shared JSON:API building blocks (components/schemas,
                   components/parameters, components/responses)
  paths/*.yaml     path fragments; keys under `paths` are merged, and a
                   fragment may also contribute `components` entries

Usage: bundle_openapi.py <src-dir> <out-file>

The output (openapi/openapi.yaml) is committed but never edited by hand:
run `make spec` after changing a fragment.
"""
import sys
from pathlib import Path

import yaml


def merge_components(dst: dict, src: dict) -> None:
    for key, section in src.items():
        if key not in dst:
            dst[key] = section
            continue
        for name, item in section.items():
            if name in dst[key]:
                sys.exit(f" duplicate component {key}/{name}")
            dst[key][name] = item


def main() -> None:
    if len(sys.argv) != 3:
        sys.exit("usage: bundle_openapi.py <src-dir> <out-file>")
    src_dir, out = Path(sys.argv[1]), Path(sys.argv[2])

    skeleton = src_dir / "openapi.yaml"
    if not skeleton.is_file():
        sys.exit(f"missing skeleton {skeleton}")
    spec = yaml.safe_load(skeleton.read_text())
    spec.setdefault("paths", {})
    spec.setdefault("components", {})

    for frag in sorted(list((src_dir / "paths").glob("*.yaml")) + [src_dir / "components.yaml"]):
        doc = yaml.safe_load(frag.read_text())
        if doc is None:
            continue
        for path, item in doc.get("paths", {}).items():
            if path in spec["paths"]:
                sys.exit(f"{frag}: duplicate path {path}")
            spec["paths"][path] = item
        merge_components(spec["components"], doc.get("components", {}))

    out.parent.mkdir(parents=True, exist_ok=True)
    with open(out, "w") as f:
        yaml.safe_dump(spec, f, sort_keys=False, allow_unicode=True, width=100)
    print(f"wrote {out} ({len(spec['paths'])} paths)")


if __name__ == "__main__":
    main()
