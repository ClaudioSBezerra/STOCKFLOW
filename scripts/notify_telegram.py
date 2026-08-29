# /// script
# dependencies = []
# ///
"""Watch bmad-loop's run state and push progress updates to Telegram.

Polls `bmad-loop status` on an interval and sends a Telegram message whenever
a story's phase changes (dev-running -> review-running -> done/blocked/etc.)
or the whole run finishes. Reads the bot token/chat id from .env.telegram
(gitignored, never commit real credentials).

Usage:
    uv run scripts/notify_telegram.py                 # poll every 30s until run finishes
    uv run scripts/notify_telegram.py --interval 60
    uv run scripts/notify_telegram.py --once           # single status push, then exit
"""
import argparse
import os
import re
import subprocess
import sys
import time
import urllib.request
import urllib.parse
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ENV_PATH = ROOT / ".env.telegram"
STATE_PATH = ROOT / "_bmad-output/implementation-artifacts/.telegram-notify-state.txt"


def load_env():
    if not ENV_PATH.exists():
        print(f"Missing {ENV_PATH} — create it with TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID.", file=sys.stderr)
        sys.exit(1)
    env = {}
    for line in ENV_PATH.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        k, v = line.split("=", 1)
        env[k.strip()] = v.strip()
    token = env.get("TELEGRAM_BOT_TOKEN")
    chat_id = env.get("TELEGRAM_CHAT_ID")
    if not token or not chat_id:
        print(f"{ENV_PATH} must define TELEGRAM_BOT_TOKEN and TELEGRAM_CHAT_ID.", file=sys.stderr)
        sys.exit(1)
    return token, chat_id


def send_telegram(token, chat_id, text):
    url = f"https://api.telegram.org/bot{token}/sendMessage"
    data = urllib.parse.urlencode({"chat_id": chat_id, "text": text}).encode()
    req = urllib.request.Request(url, data=data, method="POST")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            resp.read()
    except Exception as exc:
        print(f"Telegram send failed: {exc}", file=sys.stderr)


def get_status_text():
    result = subprocess.run(["bmad-loop", "status", "--project", str(ROOT)], capture_output=True, text=True)
    return result.stdout.strip() or result.stderr.strip()


# "  1-3-autocadastro-com-verificação-de-e-mail review-running    dev×1 review×1 ..."
STORY_LINE_RE = re.compile(r"^\s{2}(\S+)\s+(\S+)\s+.*$", re.M)
RUN_STATUS_RE = re.compile(r"^status:\s*(.+)$", re.M)
BACKLOG_RE = re.compile(r"sprint backlog remaining:\s*(\d+)")


def parse_state(text):
    stories = dict(STORY_LINE_RE.findall(text))
    run_status_match = RUN_STATUS_RE.search(text)
    backlog_match = BACKLOG_RE.search(text)
    return {
        "stories": stories,
        "run_status": run_status_match.group(1).strip() if run_status_match else None,
        "backlog": int(backlog_match.group(1)) if backlog_match else None,
    }


def load_prev_state():
    if not STATE_PATH.exists():
        return {}
    prev = {}
    for line in STATE_PATH.read_text().splitlines():
        if "=" in line:
            k, v = line.split("=", 1)
            prev[k] = v
    return prev


def save_state(stories):
    STATE_PATH.parent.mkdir(parents=True, exist_ok=True)
    STATE_PATH.write_text("\n".join(f"{k}={v}" for k, v in stories.items()) + "\n")


def diff_and_notify(token, chat_id, state):
    prev = load_prev_state()
    changed = []
    for key, phase in state["stories"].items():
        if prev.get(key) != phase:
            changed.append((key, prev.get(key), phase))

    for key, old_phase, new_phase in changed:
        if new_phase == "done":
            send_telegram(token, chat_id, f"✅ Story concluída: {key}\nBacklog restante: {state['backlog']}")
        elif new_phase in ("blocked", "escalated"):
            send_telegram(token, chat_id, f"🚨 Story precisa de atenção: {key} -> {new_phase}")
        elif old_phase is None:
            send_telegram(token, chat_id, f"▶️ Story iniciada: {key} ({new_phase})")
        # dev-running <-> review-running transitions: no push, too noisy.

    save_state(state["stories"])
    return changed


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--interval", type=int, default=30, help="poll interval in seconds (default 30)")
    parser.add_argument("--once", action="store_true", help="push current status once and exit")
    args = parser.parse_args()

    token, chat_id = load_env()

    if args.once:
        text = get_status_text()
        state = parse_state(text)
        diff_and_notify(token, chat_id, state)
        print(text)
        return

    print(f"Watching bmad-loop status every {args.interval}s. Ctrl+C to stop.")
    last_run_status = None
    try:
        while True:
            text = get_status_text()
            state = parse_state(text)
            changed = diff_and_notify(token, chat_id, state)
            for key, old, new in changed:
                print(f"{key}: {old} -> {new}")

            if state["run_status"] in ("finished", "stopped") and state["run_status"] != last_run_status:
                send_telegram(
                    token, chat_id,
                    f"🏁 Run finalizada ({state['run_status']}). Backlog restante: {state['backlog']}",
                )
                last_run_status = state["run_status"]
                break

            last_run_status = state["run_status"]
            time.sleep(args.interval)
    except KeyboardInterrupt:
        print("\nStopped.")


if __name__ == "__main__":
    main()
