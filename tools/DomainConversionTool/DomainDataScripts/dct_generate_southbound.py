#!/usr/bin/env python3

"""

dct_generate_southbound.py

 

Generate a southbound (CAN-style) domain YAML file and the corresponding

North/South mapping YAML file for the COVESA VISSR Domain Conversion Tool

(DCT), starting from a northbound (VSS) domain YAML file.

 

Southbound signal naming rule

------------------------------

For each leaf signal path in the northbound file:

  1. Take the last dot-separated segment of the path (e.g. "IsEnabled" for

     "Vehicle.ADAS.ABS.IsEnabled").

  2. If that last segment is unique among all leaf signals, use it as-is for

     the southbound name.

  3. If multiple leaf signals share the same last segment, disambiguate by

     finding the longest common LEADING prefix (segment-by-position, starting

     from the root) shared by all paths in that colliding group, stripping

     that shared prefix, and joining the remaining segments with "_".

     Example:

       Vehicle.ADAS.ABS.IsEnabled          -> ADAS_ABS_IsEnabled

       Vehicle.ADAS.CruiseControl.IsEnabled -> ADAS_CruiseControl_IsEnabled

     (Both end in "IsEnabled" which collides, so segments after the common

     "Vehicle.ADAS." prefix are kept and joined.)

 

This "common leading prefix" rule was chosen over a naive "remove any

segment value that appears in all colliding paths" interpretation, because

the naive approach can strip out segment values that recur at different

tree positions (e.g. "Front", "Row1", "DriverSide") and cause unrelated

signals to collapse onto the same southbound name, or even produce empty

names. The script verifies both of these failure modes cannot occur and

will raise an assertion error if they do.

 

Only leaf signals (type != "branch") are ever mapped or written to the

southbound file, matching the DCT's own flat southbound domain example

format (see CAN-v0.1.yaml in the vissr repo).

 

Input file format

------------------

The northbound file may be either:

  (a) a full VSS-tools tree export with branch nodes and no "Domain:" header

      (as produced by vss-tools), or

  (b) an already-flattened DCT-style file with a "Domain:" header and only

      leaf signals (e.g. as produced by dct_strip_branches.py).

Either way, only leaf (non-branch) signals are used to build the southbound

file and mapping; any "Domain:" header line in the input is ignored/skipped.

 

Usage

-----

    python dct_generate_southbound.py NORTHBOUND.yaml \\

        --nbd-name VSSv6.1 --sbd-name CANv6.1 \\

        --can-out Vehicle-CANv6.1.yaml \\

        --map-out Map-VSSv6.1-CANv6.1.yaml

 

Example (matches the files generated for Vehicle-VSSv6.1.yaml):

 

    python dct_generate_southbound.py Vehicle-VSSv6.1.yaml \\

        --nbd-name VSSv6.1 --sbd-name CANv6.1 \\

        --can-out Vehicle-CANv6.1.yaml \\

        --map-out Map-VSSv6.1-CANv6.1.yaml

 

Notes / limitations

--------------------

  - This is a lightweight line-based parser matching how the DCT Go tool

    itself reads these files. It does NOT do full YAML re-serialization, so

    original formatting/comments within each leaf block are preserved

    exactly in the generated southbound file.

  - Per the DCT README, signals with array datatypes (e.g. "string[]",

    "uint8[]") cannot be processed by DCT's "join" step. They are still

    included in the generated southbound/map files for completeness; DCT

    will skip/fail just those specific pairs during "join", which is a

    documented DCT limitation and not a bug in these generated files.

  - If your northbound file is missing a "Domain:" header (e.g. a raw

    vss-tools export), run dct_strip_branches.py on it first, or simply

    supply --nbd-name here; this script does not modify the input file.

"""

 

import argparse

import collections

import re

import sys

 

NAME_RE = re.compile(r'^([A-Za-z0-9_.]+):\s*$')

 

 

def parse_blocks(lines):

    """Split lines into (path, block_lines) tuples, one per top-level entry.

    Any "Domain: ..." line is skipped (not treated as a block)."""

    blocks = []

    current_path = None

    current_lines = []

 

    for line in lines:

        stripped = line.rstrip('\n')

        if stripped.startswith('Domain:'):

            continue

        m = NAME_RE.match(stripped) if (len(stripped) > 0 and stripped[0] != ' ') else None

        if m:

            if current_path is not None:

                blocks.append((current_path, current_lines))

            current_path = m.group(1)

            current_lines = [line]

        else:

            if current_path is not None:

                current_lines.append(line)

    if current_path is not None:

        blocks.append((current_path, current_lines))

    return blocks

 

 

def block_type(block_lines):

    for l in block_lines:

        s = l.strip()

        if s.startswith('type:'):

            return s.split(':', 1)[1].strip()

    return None

 

 

def common_prefix_len(seg_lists):

    minlen = min(len(s) for s in seg_lists)

    n = 0

    for i in range(minlen):

        vals = set(s[i] for s in seg_lists)

        if len(vals) == 1:

            n += 1

        else:

            break

    return n

 

 

def compute_can_names(leaf_paths):

    last_seg_map = collections.defaultdict(list)

    for path in leaf_paths:

        last = path.split('.')[-1]

        last_seg_map[last].append(path)

 

    can_name_of = {}

    for last, paths in last_seg_map.items():

        if len(paths) == 1:

            can_name_of[paths[0]] = last

            continue

        seg_lists = [p.split('.') for p in paths]

        prefix_len = common_prefix_len(seg_lists)

        for p, segs in zip(paths, seg_lists):

            remaining = segs[prefix_len:]

            can_name_of[p] = "_".join(remaining)

 

    # Sanity checks: fail loudly rather than silently emit broken output.

    assert len(can_name_of) == len(leaf_paths), "internal error: name count mismatch"

    empties = [p for p, n in can_name_of.items() if not n]

    assert not empties, f"empty CAN name(s) generated for: {empties}"

    counts = collections.Counter(can_name_of.values())

    dups = [n for n, c in counts.items() if c > 1]

    assert not dups, f"duplicate CAN names generated: {dups}"

 

    return can_name_of

 

 

def main():

    parser = argparse.ArgumentParser(description=__doc__,

                                      formatter_class=argparse.RawDescriptionHelpFormatter)

    parser.add_argument('input', help='Path to the northbound domain YAML file')

    parser.add_argument('--nbd-name', required=True, help='Northbound domain name (e.g. VSSv6.1)')

    parser.add_argument('--sbd-name', required=True, help='Southbound domain name (e.g. CANv6.1)')

    parser.add_argument('--can-out', required=True, help='Output path for the southbound domain YAML file')

    parser.add_argument('--map-out', required=True, help='Output path for the North/South mapping YAML file')

    args = parser.parse_args()

 

    with open(args.input, encoding='utf-8') as f:

        lines = f.readlines()

 

    blocks = parse_blocks(lines)

    leaf_blocks = [(p, bl) for p, bl in blocks if block_type(bl) != 'branch']

    branch_count = len(blocks) - len(leaf_blocks)

 

    leaf_paths = [p for p, _ in leaf_blocks]

    can_name_of = compute_can_names(leaf_paths)

 

    # --- Write southbound domain file ---

    with open(args.can_out, 'w', encoding='utf-8', newline='\n') as out:

        out.write(f"Domain: {args.sbd_name}\n\n")

        for path, blines in leaf_blocks:

            can_name = can_name_of[path]

            out.write(f"{can_name}:\n")

            for l in blines[1:]:

                out.write(l)

 

    # --- Write mapping file ---

    with open(args.map_out, 'w', encoding='utf-8', newline='\n') as out:

        out.write("# DCT-table-names\n")

        out.write(f"NorthBoundDomain: {args.nbd_name}\n")

        out.write(f"SouthBoundDomain: {args.sbd_name}\n\n")

        out.write("Mapping:\n")

        for path in leaf_paths:

            out.write(f"- North: {path}\n")

            out.write(f"  South: {can_name_of[path]}\n")

 

    print(f"Total top-level blocks parsed: {len(blocks)}")

    print(f"Branch blocks skipped:         {branch_count}")

    print(f"Leaf signals mapped:           {len(leaf_paths)}")

    print(f"Southbound file written to:    {args.can_out}")

    print(f"Mapping file written to:       {args.map_out}")

 

 

if __name__ == '__main__':

    main()
