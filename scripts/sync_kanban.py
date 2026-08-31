# /// script
# dependencies = ["pyyaml"]
# ///
"""Sync the GitHub Project kanban (#5) to match _bmad-output/implementation-artifacts/sprint-status.yaml.

bmad-loop only ever updates sprint-status.yaml as it works through stories — it
has no idea the GitHub Project board exists. This script is the missing link:
run it whenever you want the kanban cards to reflect real dev/review progress.

Usage:
    uv run scripts/sync_kanban.py            # sync, only touching what changed
    uv run scripts/sync_kanban.py --dry-run  # preview without calling gh
    uv run scripts/sync_kanban.py --force    # re-sync every mapped item, ignoring the cache

Requires: `gh` authenticated with the `project` scope (already the case in this repo).
"""
import argparse
import json
import re
import subprocess
import sys
from pathlib import Path

import yaml

REPO = "ClaudioSBezerra/STOCKFLOW"
PROJECT_ID = "PVT_kwHOBon9h84Bh1SU"
STATUS_FIELD_ID = "PVTSSF_lAHOBon9h84Bh1SUzhgvjWg"

STATUS_OPTION_IDS = {
    "Backlog": "f75ad846",
    "Ready": "61e4505c",
    "In progress": "47fc9ee4",
    "In review": "df73e18b",
    "Done": "98236657",
}

STORY_STATUS_MAP = {
    "backlog": "Backlog",
    "ready-for-dev": "Ready",
    "in-progress": "In progress",
    "review": "In review",
    "done": "Done",
    # bmad-loop's terminal-but-not-done park: agent-doable work is committed,
    "awaiting-operator": "In review",
    # but acceptance criteria include an action only a human can do (e.g.
    # provisioning a real external credential). Closest board analog to
    # "waiting on a human before this can truly be Done" -- the issue stays
    # open (done=False below) so it doesn't read as finished.
}

EPIC_STATUS_MAP = {
    "backlog": "Backlog",
    "in-progress": "In progress",
    "done": "Done",
}

ROOT = Path(__file__).resolve().parent.parent
SPRINT_STATUS_PATH = ROOT / "_bmad-output/implementation-artifacts/sprint-status.yaml"
ISSUE_MAP_PATH = ROOT / "_bmad-output/implementation-artifacts/github-issue-map.json"
CACHE_PATH = ROOT / "_bmad-output/implementation-artifacts/.kanban-sync-state.json"

STORY_KEY_RE = re.compile(r"^(\d+)-(\d+)-")
EPIC_RETRO_RE = re.compile(r"^epic-\d+-retrospective$")


def run(cmd):
    result = subprocess.run(cmd, capture_output=True, text=True)
    if result.returncode != 0:
        raise RuntimeError(f"command failed: {' '.join(cmd)}\n{result.stderr}")
    return result.stdout


def load_cache():
    if CACHE_PATH.exists():
        return json.loads(CACHE_PATH.read_text())
    return {}


def save_cache(cache):
    CACHE_PATH.write_text(json.dumps(cache, indent=2, ensure_ascii=False) + "\n")


def resolve_issue(key, issue_map):
    """Map a sprint-status.yaml key to (kind, github_entry) or (None, None) if unmapped."""
    if key == "epic-list-note":
        return None, None
    if EPIC_RETRO_RE.match(key):
        return None, None  # retrospectives have no GitHub issue
    if key.startswith("epic-"):
        entry = issue_map.get("epics", {}).get(key)
        return ("epic", entry) if entry else (None, None)
    m = STORY_KEY_RE.match(key)
    if m:
        story_key = f"{m.group(1)}.{m.group(2)}"
        entry = issue_map.get("stories", {}).get(story_key)
        return ("story", entry) if entry else (None, None)
    return None, None


def set_status(item_id, status_name, dry_run):
    option_id = STATUS_OPTION_IDS[status_name]
    if dry_run:
        return
    run([
        "gh", "project", "item-edit",
        "--id", item_id,
        "--project-id", PROJECT_ID,
        "--field-id", STATUS_FIELD_ID,
        "--single-select-option-id", option_id,
    ])


def get_issue_state(issue_number):
    out = run(["gh", "issue", "view", str(issue_number), "--repo", REPO, "--json", "state"])
    return json.loads(out)["state"]  # "OPEN" or "CLOSED"


def set_issue_open_state(issue_number, done, dry_run):
    if dry_run:
        return
    # gh issue close/reopen errors with a generic GraphQL message when the
    # issue is already in the target state — check first so a stale cache
    # (or a crash-lost prior run) never turns into a hard failure.
    current = get_issue_state(issue_number)
    target = "CLOSED" if done else "OPEN"
    if current == target:
        return
    cmd = "close" if done else "reopen"
    try:
        run(["gh", "issue", cmd, str(issue_number), "--repo", REPO])
    except RuntimeError:
        # Observed once (issue #27, 2026-08-31): gh reported "Could not
        # close/reopen the issue" (GraphQL) even though the mutation had
        # already landed server-side — a write-succeeded/ack-failed race,
        # not a real failure. Re-check state before propagating: if the
        # server already reflects the target, the mutation worked and the
        # local error was noise. Only a genuine mismatch re-raises.
        if get_issue_state(issue_number) != target:
            raise


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dry-run", action="store_true", help="preview without calling gh")
    parser.add_argument("--force", action="store_true", help="re-sync every mapped item, ignoring the cache")
    args = parser.parse_args()

    sprint_status = yaml.safe_load(SPRINT_STATUS_PATH.read_text())
    dev_status = sprint_status.get("development_status", {})
    issue_map = json.loads(ISSUE_MAP_PATH.read_text())
    cache = {} if args.force else load_cache()

    changed, skipped, unmapped = [], [], []

    for key, status in dev_status.items():
        kind, entry = resolve_issue(key, issue_map)
        if entry is None:
            if not (key.startswith("epic-") and key.endswith("-retrospective")):
                unmapped.append(key)
            continue

        status_map = EPIC_STATUS_MAP if kind == "epic" else STORY_STATUS_MAP
        target_status = status_map.get(status)
        if target_status is None:
            unmapped.append(f"{key} (unrecognized status '{status}')")
            continue

        if cache.get(key) == status:
            skipped.append(key)
            continue

        set_status(entry["project_item_id"], target_status, args.dry_run)
        set_issue_open_state(entry["issue_number"], done=(status == "done"), dry_run=args.dry_run)
        changed.append((key, status, target_status, entry["issue_number"]))
        cache[key] = status
        if not args.dry_run:
            # Save after every item, not just at the end — a crash partway
            # through (network blip, gh API hiccup) must not force every
            # already-synced item to be re-attempted on the next run.
            save_cache(cache)

    print(f"{'[dry-run] ' if args.dry_run else ''}Synced {len(changed)} item(s), {len(skipped)} already in sync.")
    for key, status, target_status, issue_number in changed:
        print(f"  #{issue_number:<4} {key:<55} -> {status} (board: {target_status})")
    if unmapped:
        print(f"\n{len(unmapped)} key(s) in sprint-status.yaml with no GitHub issue mapping (ignored):")
        for key in unmapped:
            print(f"  {key}")


if __name__ == "__main__":
    main()
