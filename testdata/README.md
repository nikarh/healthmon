# Test Data

This directory contains capture scenarios and their replay dumps.

Structure:
- `testdata/scenarios/` contains TOML scenario files.
- `testdata/dumps/` contains generated `.jsonl` files (`*.events.jsonl` and `*.inspects.jsonl`).

Capture workflow:
1. Run: `python3 scripts/capture_scenarios.py`

The capture script cleans up containers labeled `healthmon.test=1` before/after each scenario.
