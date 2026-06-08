#!/usr/bin/env bash
# tools/gen-sdl.sh — regenerate VDM SDL from a reference VSS catalog.
#
# This script is called by the SDL-drift CI job (.github/workflows/s2dm-validate.yml).
# It is a template; fill in the steps appropriate for your project.
#
# Prerequisites:
#   uv   (installed by CI; see https://github.com/astral-sh/uv)
#   vss-tools (installed below)
#   covesa-s2dm (installed below)
#
# Usage:
#   bash tools/gen-sdl.sh
#
# Outputs: updates *.graphql files in the paths configured below.

set -euo pipefail

# ── Configuration ──────────────────────────────────────────────────────────────
# Path to the VSS YAML catalog relative to the repo root.
VSS_CATALOG="${VSS_CATALOG:-spec/vss-catalog.yaml}"

# Directory where generated SDL files should be written.
SDL_OUTPUT_DIR="${SDL_OUTPUT_DIR:-server/vissv2server/vdmloader/generated}"

# ── Safety guard ────────────────────────────────────────────────────────────────
if [ ! -f "$VSS_CATALOG" ]; then
    echo "VSS catalog not found at $VSS_CATALOG"
    echo "Set VSS_CATALOG env var to the path of your VSS YAML catalog."
    exit 0  # Soft exit — drift check treats missing catalog as "skip"
fi

# ── Install tools ───────────────────────────────────────────────────────────────
echo "Installing vss-tools..."
uv tool install vss-tools

echo "Installing covesa-s2dm..."
uv tool install covesa-s2dm

# ── Generate SDL ────────────────────────────────────────────────────────────────
echo "Generating SDL from $VSS_CATALOG → $SDL_OUTPUT_DIR ..."
mkdir -p "$SDL_OUTPUT_DIR"

# vss-tools exports to VDM/SDL format.  Adjust flags for your vss-tools version.
uvx vss-tools "$VSS_CATALOG" \
    --format sdl \
    --output "$SDL_OUTPUT_DIR"

echo "SDL generation complete. Files written to $SDL_OUTPUT_DIR"
