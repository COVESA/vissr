#!/usr/bin/env python3

"""

dct_strip_branches.py

 

Convert a full VSS-tools YAML tree (with "branch" nodes and no "Domain:"

header) into the flat leaf-only format expected as northbound domain input

for the COVESA VISSR Domain Conversion Tool (DCT).

 

What it does:

  1. Parses the input YAML file as a sequence of top-level blocks, where each

     block starts at a line of the form "SomePath:" (no leading whitespace)

     and continues until the next such line.

  2. Drops every block whose "type:" field equals "branch".

  3. Writes a new file starting with a "Domain: <name>" header line, followed

     by the remaining (leaf) blocks in their original order and formatting.

 

This mirrors the flat signal-description format used by the DCT's own

example files (see VSS-v0.1.yaml / CAN-v0.1.yaml in the vissr repo).

 

Usage:

    python dct_strip_branches.py INPUT.yaml DOMAIN_NAME [-o OUTPUT.yaml] [--in-place]

 

Examples:

    # Write to a new file, leaving the input untouched

    python dct_strip_branches.py Vehicle-VSSv6.1.yaml VSSv6.1 -o Vehicle-VSSv6.1-flat.yaml

 

    # Overwrite the input file directly (make your own backup first!)

    python dct_strip_branches.py Vehicle-VSSv6.1.yaml VSSv6.1 --in-place

 

Notes / limitations:

  - This is a lightweight line-based parser matching how the DCT Go tool

    itself reads these files (see populateTable() in DomainConversionTool.go).

    It does NOT do full YAML re-serialization, so original formatting,

    comments, and key order within each block are preserved exactly.

  - A "block" is only recognized if its header line matches ^[A-Za-z0-9_.]+:$

    (no leading whitespace, nothing after the colon). This matches standard

    VSS-tools YAML export format.

  - If the input file already has a "Domain:" line, it will be removed and

    replaced with the one you specify here.

"""

 

import argparse

import re

import sys

 

NAME_RE = re.compile(r'^([A-Za-z0-9_.]+):\s*$')

 

 

def parse_blocks(lines):

    """Split lines into (path, block_lines) tuples, one per top-level entry.

    Any leading "Domain: ..." line is skipped (not treated as a block)."""

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

 

 

def main():

    parser = argparse.ArgumentParser(description=__doc__,

                                      formatter_class=argparse.RawDescriptionHelpFormatter)

    parser.add_argument('input', help='Path to the full VSS tree YAML file')

    parser.add_argument('domain', help='Domain name to write in the "Domain:" header (e.g. VSSv6.1)')

    parser.add_argument('-o', '--output', help='Output file path (default: print summary only)')

    parser.add_argument('--in-place', action='store_true',

                         help='Overwrite the input file directly. Make your own backup first!')

    args = parser.parse_args()

 

    if args.in_place and args.output:

        print('ERROR: --in-place and -o/--output are mutually exclusive.', file=sys.stderr)

        sys.exit(1)

    if not args.in_place and not args.output:

        print('ERROR: specify either -o OUTPUT.yaml or --in-place.', file=sys.stderr)

        sys.exit(1)

 

    with open(args.input, encoding='utf-8') as f:

        lines = f.readlines()

 

    blocks = parse_blocks(lines)

    leaf_blocks = [(p, bl) for p, bl in blocks if block_type(bl) != 'branch']

    branch_count = len(blocks) - len(leaf_blocks)

 

    out_path = args.input if args.in_place else args.output

 

    with open(out_path, 'w', encoding='utf-8', newline='\n') as out:

        out.write(f"Domain: {args.domain}\n\n")

        for _path, blines in leaf_blocks:

            for l in blines:

                out.write(l)

 

    print(f"Total top-level blocks parsed: {len(blocks)}")

    print(f"Branch blocks removed:         {branch_count}")

    print(f"Leaf blocks written:           {len(leaf_blocks)}")

    print(f"Output written to:             {out_path}")

 

 

if __name__ == '__main__':

    main()
