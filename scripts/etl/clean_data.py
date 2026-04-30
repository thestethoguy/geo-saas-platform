#!/usr/bin/env python3
"""
clean_data.py — India Villages Dataset ETL Pipeline
=====================================================
Reads all .xls and .ods files from data/raw_dataset/, merges them into a
single master DataFrame, and exports to data/processed/master_india_villages.csv.

Usage:
    python scripts/etl/clean_data.py

Dependencies:
    pip install pandas xlrd odfpy
"""

import os
import sys
import pathlib
import pandas as pd

# ── Paths (relative to project root) ──────────────────────────────────────────
PROJECT_ROOT  = pathlib.Path(__file__).resolve().parents[2]
RAW_DIR       = PROJECT_ROOT / "data" / "raw_dataset"
OUTPUT_DIR    = PROJECT_ROOT / "data" / "processed"
OUTPUT_FILE   = OUTPUT_DIR / "master_india_villages.csv"

# ── Supported extensions → pandas engine ──────────────────────────────────────
ENGINE_MAP = {
    ".xls":  "xlrd",   # requires: pip install xlrd
    ".xlsx": "openpyxl", # requires: pip install openpyxl (bonus support)
    ".ods":  "odf",    # requires: pip install odfpy
}

# ── ANSI colour codes for readable console output ─────────────────────────────
GREEN  = "\033[92m"
YELLOW = "\033[93m"
RED    = "\033[91m"
CYAN   = "\033[96m"
BOLD   = "\033[1m"
RESET  = "\033[0m"

def log_info(msg:  str) -> None: print(f"  {GREEN}✔{RESET}  {msg}")
def log_warn(msg:  str) -> None: print(f"  {YELLOW}⚠{RESET}  {msg}")
def log_error(msg: str) -> None: print(f"  {RED}✘{RESET}  {msg}")
def log_head(msg:  str) -> None: print(f"\n{BOLD}{CYAN}{msg}{RESET}")


def is_junk(path: pathlib.Path) -> bool:
    """Return True for macOS resource-fork files and hidden directories."""
    # Files whose name starts with ._ are macOS AppleDouble resource forks
    if path.name.startswith("._"):
        return True
    # Anything inside __MACOSX is metadata, not data
    if "__MACOSX" in path.parts:
        return True
    # Any hidden file / directory (starts with a single dot)
    if path.name.startswith("."):
        return True
    return False


def collect_files(root: pathlib.Path) -> list[pathlib.Path]:
    """Recursively collect all supported data files, skipping junk."""
    found = []
    for path in sorted(root.rglob("*")):
        if path.is_dir():
            continue
        if is_junk(path):
            log_warn(f"Skipping junk file : {path.relative_to(root)}")
            continue
        ext = path.suffix.lower()
        if ext not in ENGINE_MAP:
            log_warn(f"Unsupported format  : {path.relative_to(root)}  (skipped)")
            continue
        found.append(path)
    return found


def read_file(path: pathlib.Path) -> pd.DataFrame | None:
    """Read a single .xls or .ods file into a DataFrame."""
    ext    = path.suffix.lower()
    engine = ENGINE_MAP[ext]

    print(f"\n  {CYAN}►{RESET} Processing : {BOLD}{path.name}{RESET}")
    print(f"     Format   : {ext}  |  Engine: {engine}")

    try:
        df = pd.read_excel(path, engine=engine, header=0, dtype=str)

        # ── Basic cleanup ────────────────────────────────────────────────────
        # Strip leading/trailing whitespace from every string cell
        df = df.apply(lambda col: col.str.strip() if col.dtype == "object" else col)

        # Drop rows that are entirely NaN (blank spacer rows common in census files)
        df.dropna(how="all", inplace=True)

        # Drop columns that are entirely NaN (blank spacer columns)
        df.dropna(axis=1, how="all", inplace=True)

        # Attach provenance: which source file did this row come from?
        df["_source_file"] = path.name

        log_info(f"Loaded {len(df):,} rows  ×  {len(df.columns) - 1} data columns")
        return df

    except ImportError as exc:
        log_error(
            f"Missing dependency for {ext}: {exc}\n"
            f"     Run:  pip install {engine if engine != 'odf' else 'odfpy'}"
        )
        return None
    except Exception as exc:
        log_error(f"Failed to read {path.name}: {exc}")
        return None


def normalise_columns(df: pd.DataFrame) -> pd.DataFrame:
    """
    Lowercase and snake_case all column names so files with slightly
    different header capitalisation merge cleanly.
    """
    df.columns = (
        df.columns
        .str.strip()
        .str.lower()
        .str.replace(r"[\s\-/]+", "_", regex=True)   # spaces/hyphens → underscore
        .str.replace(r"[^\w]", "", regex=True)         # strip remaining special chars
    )
    return df


def main() -> None:
    log_head("━━━  India Villages ETL Pipeline  ━━━")
    print(f"  Raw data   : {RAW_DIR}")
    print(f"  Output     : {OUTPUT_FILE}")

    # ── 1. Validate raw directory ─────────────────────────────────────────────
    if not RAW_DIR.exists():
        log_error(
            f"Raw dataset directory not found: {RAW_DIR}\n"
            "  Create it and place your .xls / .ods files inside."
        )
        sys.exit(1)

    # ── 2. Collect eligible files ─────────────────────────────────────────────
    log_head("Step 1 — Scanning raw_dataset/")
    files = collect_files(RAW_DIR)

    if not files:
        log_error("No supported files found in raw_dataset/. Nothing to do.")
        sys.exit(1)

    print(f"\n  Found {len(files)} file(s) to process.")

    # ── 3. Read and collect DataFrames ────────────────────────────────────────
    log_head("Step 2 — Reading files")
    frames: list[pd.DataFrame] = []

    for path in files:
        df = read_file(path)
        if df is not None:
            df = normalise_columns(df)
            frames.append(df)

    if not frames:
        log_error("All files failed to load. Check the warnings above.")
        sys.exit(1)

    # ── 4. Merge into master DataFrame ────────────────────────────────────────
    log_head("Step 3 — Merging into master DataFrame")
    master = pd.concat(frames, ignore_index=True, sort=False)

    log_info(f"Total rows   : {len(master):,}")
    log_info(f"Total columns: {len(master.columns)}")
    log_info(f"Columns      : {', '.join(master.columns.tolist())}")

    # ── 4. Transform — rename + clean columns ────────────────────────────────
    log_head("Step 4 — Transforming columns")

    # Map raw census column headers → schema names expected by the Go backend.
    # Only keys present in the DataFrame are renamed; missing keys are silently ignored.
    RENAME_MAP = {
        "mdds_stc":    "state_lgd_code",
        "mdds_dtc":    "district_lgd_code",
        "mdds_sub_dt": "sub_district_lgd_code",
        "mdds_plcn":   "village_lgd_code",
        "area_name":   "village_name",
    }
    master.rename(columns=RENAME_MAP, inplace=True)
    renamed = [f"{old} → {new}" for old, new in RENAME_MAP.items() if old in master.columns or new in master.columns]
    log_info(f"Renamed  : {', '.join(renamed) if renamed else 'nothing to rename (columns may already be correct)'}")

    # Drop junk columns: anything whose name contains 'unnamed' or '_source_file'.
    junk_cols = [
        c for c in master.columns
        if "unnamed" in c.lower() or c == "_source_file"
    ]
    if junk_cols:
        master.drop(columns=junk_cols, inplace=True)
        log_info(f"Dropped  : {', '.join(junk_cols)}")
    else:
        log_info("Dropped  : no junk columns found")

    log_info(f"Final columns ({len(master.columns)}): {', '.join(master.columns.tolist())}")

    # ── 5. Export to CSV ──────────────────────────────────────────────────────
    log_head("Step 5 — Exporting to CSV")
    OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

    # Strip any lingering whitespace / BOM artefacts from column headers before
    # writing. This is the last line of defence against invisible characters
    # (e.g. \ufeff prepended by utf-8-sig) that would cause the Go CSV reader
    # to report "missing required column \"state_lgd_code\"" even though the
    # column appears to exist.
    master.columns = master.columns.str.strip()
    log_info(f"Header strip applied. Final columns: {', '.join(master.columns.tolist())}")

    # Use strict UTF-8 — NO BOM.  utf-8-sig would write 0xEF 0xBB 0xBF at byte
    # 0, silently corrupting the first column header for non-Excel consumers.
    master.to_csv(OUTPUT_FILE, index=False, encoding="utf-8")

    size_kb = OUTPUT_FILE.stat().st_size / 1024
    log_info(f"Saved  → {OUTPUT_FILE}  ({size_kb:,.1f} KB)")

    # ── 6. Summary ────────────────────────────────────────────────────────────
    log_head("━━━  ETL Complete  ━━━")
    print(f"""
  Files processed : {len(frames)} / {len(files)}
  Total rows      : {len(master):,}
  Output file     : {OUTPUT_FILE.relative_to(PROJECT_ROOT)}
  Encoding        : UTF-8 (no BOM — safe for Go/CLI consumers)

  Next step → run the ingestion script to load into Postgres + Typesense:
    cd scripts/ingest && go run main.go
""")


if __name__ == "__main__":
    main()
