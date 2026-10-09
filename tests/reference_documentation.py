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

Two entry points:

  reference_documentation.py payloads SOURCE REFERENCE
  reference_documentation.py vocabulary LABEL HEADING SOURCE SUFFIX REFERENCE [--rows]

The second holds a published vocabulary to the section that owns it, for the
same reason and in the same way. Measured 2026-10-09: the row explaining the
component state `degraded` was deleted from its own section and the gate passed,
because the word survives where it counts degraded transports. A check keyed on
the word cannot notice an explanation being removed.

`--rows` adds the reverse direction, that every row in the section is a value of
the vocabulary. It is given only to the sections whose rows are their
vocabulary: the component names, the component states, the classifications and
the proposal classes. `## Authorization` tabulates the authorization reasons and
explains its own two values in prose, `## The diff` tabulates the
classifications and explains its twelve reasons in prose, and the declared
sources sit in the second column of the component table — for those, requiring
rows would mean rewriting the document to satisfy a gate rather than a reader.
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
    ends = [index for index in (reference.find("\n## ", start),
                                reference.find("\n### ", start),
                                reference.find("\n#### ", start)) if index > 0]
    return reference[start:min(ends)] if ends else reference[start:]


def vocabulary(
    label: str,
    heading: str,
    source_path: str,
    suffix: str,
    reference_path: str,
    rows_are_the_vocabulary: bool,
) -> int:
    """Hold one published vocabulary to the section that owns it."""
    source = open(source_path).read()
    reference = open(reference_path).read()

    # Two shapes are declared in this code: a typed constant block, and the
    # source table, whose first column is the value. The suffix names which.
    if suffix == "@sources":
        values = sorted(set(re.findall(r'\{"([a-z]+\.[a-z]+)"', source)))
    else:
        values = sorted(set(re.findall(rf'{re.escape(suffix)} = "([a-z_.]+)"', source)))
    if not values:
        print(f"{label}: no values were read from {source_path} as {suffix};",
              "this check is not looking at anything", file=sys.stderr)
        return 1

    start = reference.find(heading)
    if start < 0:
        # Widening to the whole document is the defect being removed, so an
        # absent section is a refusal rather than a fallback.
        print(f"{label}: the section {heading!r} is not in {reference_path}", file=sys.stderr)
        return 1
    start += len(heading)
    # A section ends at the next heading of any level. Bounding only on ## and
    # ### let a #### subsection swallow the next one's table, which is exactly
    # the mixing this keying exists to prevent.
    ends = [index for index in (reference.find("\n## ", start),
                                reference.find("\n### ", start),
                                reference.find("\n#### ", start)) if index > 0]
    section = reference[start:min(ends)] if ends else reference[start:]

    status = 0
    for value in values:
        if f"`{value}`" not in section:
            print(f"{label} {value} is not explained under {heading!r}", file=sys.stderr)
            status = 1
    if rows_are_the_vocabulary:
        declared = set(values)
        for row in sorted(set(ROW.findall(section))):
            if row not in declared:
                print(f"{label}: `{row}` is explained under {heading!r} and is not a value of it",
                      file=sys.stderr)
                status = 1
    if status != 0:
        return 1
    print(f"ok: every {label} is explained under its own section "
          f"({len(values)} values)")
    return 0


def payloads(source_path: str, reference_path: str) -> int:
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
    arguments = sys.argv[1:]
    if len(arguments) == 3 and arguments[0] == "payloads":
        raise SystemExit(payloads(arguments[1], arguments[2]))
    if arguments and arguments[0] == "vocabulary":
        # The flag is read wherever it sits, so a caller assembling the command
        # need not know where to put it.
        rows = "--rows" in arguments
        positional = [argument for argument in arguments[1:] if argument != "--rows"]
        if len(positional) == 5:
            raise SystemExit(vocabulary(*positional, rows))
    print(__doc__, file=sys.stderr)
    raise SystemExit(2)
