#!/usr/bin/env python3
"""Every quantity a component payload carries is explained under that component.

The reference's own vocabularies overlap, so a check for a backtick-quoted word
anywhere in the document passes for the wrong reason: `configured` names routes,
transports and relays with a different subject in each, `ready` and `degraded`
are both transport counts and component states, and `missing` is both a
scoped-routes quantity and a diff reason. Measured 2026-10-09, a flat check
called TransportsPayload fully explained because its three words matched rows
about other things.

So a field is looked for only between its component's heading and the next one.
The payloads, their components and their fields all come from the code: the
Payload struct's json tags are the component names and they name each payload
type, so this holds no list of its own.

Usage: payload_documentation.py SOURCE REFERENCE
"""
import re
import sys

PAYLOAD_STRUCT = re.compile(r"type Payload struct \{(.*?)\n\}", re.S)
PAYLOAD_FIELD = re.compile(r"\*(\w+Payload)\s+`json:\"([a-z_]+)")
PAYLOAD_TYPE = re.compile(r"type (\w+Payload) struct \{(.*?)\n\}", re.S)
JSON_TAG = re.compile(r'json:"([a-z_]+)')
ROW = re.compile(r"^\| `([a-z_]+)` \|", re.M)


def sections(reference: str, component: str) -> str | None:
    """The reference text under one component's payload heading."""
    heading = f"### The `{component}` payload"
    start = reference.find(heading)
    if start < 0:
        return None
    start += len(heading)
    end = reference.find("\n### ", start)
    return reference[start:] if end < 0 else reference[start:end]


def main(source_path: str, reference_path: str) -> int:
    source = open(source_path).read()
    reference = open(reference_path).read()

    container = PAYLOAD_STRUCT.search(source)
    if container is None:
        print("the Payload struct was not found; this gate is not looking at anything",
              file=sys.stderr)
        return 1
    components = {
        payload_type: component
        for payload_type, component in PAYLOAD_FIELD.findall(container.group(1))
    }
    bodies = dict(PAYLOAD_TYPE.findall(source))

    if not components:
        print("no component payloads were read from the Payload struct;",
              "this gate is not looking at anything", file=sys.stderr)
        return 1

    status = 0
    fields_held = 0
    for payload_type, component in sorted(components.items(), key=lambda pair: pair[1]):
        body = bodies.get(payload_type)
        if body is None:
            print(f"{component}: {payload_type} is named in Payload and declared nowhere",
                  file=sys.stderr)
            status = 1
            continue
        fields = JSON_TAG.findall(body)
        if not fields:
            print(f"{component}: {payload_type} declares no field, which is a broken read",
                  file=sys.stderr)
            status = 1
            continue
        section = sections(reference, component)
        if section is None:
            print(f"{component}: no `### The \\`{component}\\` payload` section explains it",
                  file=sys.stderr)
            status = 1
            continue
        explained = set(ROW.findall(section))
        for field in fields:
            if field not in explained:
                print(f"{component}.{field} is not explained under its own component",
                      file=sys.stderr)
                status = 1
            else:
                fields_held += 1
        # A row for a quantity nothing reports would describe the document's
        # own past rather than the code.
        for row in sorted(explained - set(fields)):
            print(f"{component}: `{row}` is explained and no field of {payload_type} carries it",
                  file=sys.stderr)
            status = 1

    if status != 0:
        return 1
    print(f"ok: every quantity is explained under its own component "
          f"({len(components)} payloads, {fields_held} fields)")
    return 0


if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        raise SystemExit(2)
    raise SystemExit(main(sys.argv[1], sys.argv[2]))
