# DCT input file generation scripts

These two Python scripts help prepare input files for the COVESA VISSR
[Domain Conversion Tool (DCT)](https://covesa.github.io/vissr/tools/#domain-conversion-tool),
starting from a full VSS-tools YAML tree export.

They require Python 3 and the `pyyaml` package (`pip install pyyaml`).

## Overview of the DCT input files

The DCT takes three YAML files as input:

1. **Northbound domain file** — signal descriptions for the VSS domain, flat
   (leaf signals only, no branch nodes), with a `Domain: <name>` header.
2. **Southbound domain file** — signal descriptions for the vehicle/CAN
   domain, same flat format, with its own `Domain: <name>` header.
3. **Mapping file** — pairs up `North:`/`South:` signal names between the two
   domains.

A raw VSS-tools export (e.g. `Vehicle-VSSv6.1.yaml`) is a full tree with
branch nodes and no `Domain:` header, so it needs to be flattened before use
as DCT northbound input. See `dct_strip_branches.py` below.

## Scripts

### 1. `dct_strip_branches.py`

Flattens a full VSS-tools YAML tree into DCT's northbound format:
removes all `type: branch` nodes and adds a `Domain: <name>` header.

```powershell
# Write to a new file (recommended, keeps your original export untouched)
python scripts\dct_strip_branches.py Vehicle-VSSv7.0.yaml VSSv7.0 -o Vehicle-VSSv7.0-flat.yaml

# Or overwrite the input file in place (make your own backup first!)
python scripts\dct_strip_branches.py Vehicle-VSSv7.0.yaml VSSv7.0 --in-place
```

Run `python scripts\dct_strip_branches.py --help` for full details.

### 2. `dct_generate_southbound.py`

Takes a (flattened or full-tree) northbound YAML file and generates:
- a southbound domain YAML file (e.g. `Vehicle-CANv7.0.yaml`), with signal
  names derived from the northbound paths (see naming rule below), and
- a mapping YAML file (e.g. `Map-VSSv7.0-CANv7.0.yaml`) pairing every
  northbound signal with its generated southbound name.

```powershell
python scripts\dct_generate_southbound.py Vehicle-VSSv7.0.yaml `
    --nbd-name VSSv7.0 --sbd-name CANv7.0 `
    --can-out Vehicle-CANv7.0.yaml `
    --map-out Map-VSSv7.0-CANv7.0.yaml
```

Run `python scripts\dct_generate_southbound.py --help` for full details.

Only leaf signals (non-branch) are ever included in the southbound/mapping
output, even if you pass in a full tree with branch nodes still present.

#### Southbound naming rule

For each northbound leaf signal path:
- If its last dot-separated segment (e.g. `IsEnabled` in
  `Vehicle.ADAS.ABS.IsEnabled`) is unique among all leaf signals, that
  segment alone becomes the southbound name.
- If multiple signals share the same last segment, the script finds the
  longest common **leading** prefix (matched position-by-position from the
  root) shared by all colliding paths, strips that prefix, and joins the
  remaining segments with `_`. For example:
  - `Vehicle.ADAS.ABS.IsEnabled` -> `ADAS_ABS_IsEnabled`
  - `Vehicle.ADAS.CruiseControl.IsEnabled` -> `ADAS_CruiseControl_IsEnabled`

The script asserts that the result has no empty names and no duplicate
names; if either happens (which should not occur with well-formed VSS
trees), it will raise an error rather than silently produce a broken
mapping.

## Typical workflow for a new VSS release

1. Drop the new VSS-tools export into this directory, e.g.
   `Vehicle-VSSv7.0.yaml`.
2. Flatten it into DCT northbound format:
   ```powershell
   python scripts\dct_strip_branches.py Vehicle-VSSv7.0.yaml VSSv7.0 --in-place
   ```
   (Keep your own backup of the original full-tree export if you might need
   it again later — the DCT process only needs the flattened version.)
3. Generate the southbound domain + mapping files:
   ```powershell
   python scripts\dct_generate_southbound.py Vehicle-VSSv7.0.yaml `
       --nbd-name VSSv7.0 --sbd-name CANv7.0 `
       --can-out Vehicle-CANv7.0.yaml `
       --map-out Map-VSSv7.0-CANv7.0.yaml
   ```
4. Feed all three files (`Vehicle-VSSv7.0.yaml`, `Vehicle-CANv7.0.yaml`,
   `Map-VSSv7.0-CANv7.0.yaml`) into the DCT following its own README
   (import northbound, import southbound, join, createfiles).

## Known limitations (inherited from DCT itself)

- Per the DCT README, signals with array datatypes (`string[]`, `uint8[]`,
  etc.) cannot be processed by DCT's `join` step. They are still included in
  the generated southbound/mapping files for completeness; DCT will
  skip/fail just those specific signal pairs during `join`.
- These scripts use simple line-based parsing (matching how the DCT Go tool
  itself reads these files), not a full YAML round-trip, so unusual
  formatting outside the standard VSS-tools export style may not be handled.
