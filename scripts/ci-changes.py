#!/usr/bin/env python3
"""Select component checks from the exact event diff; missing history runs all."""
import json
import os
from pathlib import Path
import re
import subprocess

COMPONENTS = ("go", "native", "android", "frontend", "shell", "desktop")


def classify(paths):
    selected = {name: False for name in COMPONENTS}
    for raw in paths:
        p = raw.replace("\\", "/")
        if p in ("scripts/ci-changes.py", "scripts/tests/test_ci_changes.py") or p.startswith(".github/actions/changed-components/"):
            return dict.fromkeys(COMPONENTS, True)
        if p == ".github/workflows/check.yml":
            for name in ("go", "frontend", "shell"):
                selected[name] = True
        if p == ".github/workflows/build-artifacts.yml":
            selected["native"] = selected["android"] = True
        if p in (".github/workflows/desktop-windows.yml", ".github/workflows/desktop-linux.yml", ".github/workflows/desktop-macos.yml"):
            selected["desktop"] = True
        if p.endswith(".md") or p.startswith(("docs/", "images/")):
            continue
        if p.startswith("desktop/"):
            selected["desktop"] = True
            continue
        if p in ("go.mod", "go.sum") or (p.endswith(".go") and ("/" not in p or p.startswith(("cmd/", "internal/", "tests/blackbox/", "tests/mobile/fixture/")))):
            selected["go"] = selected["native"] = True
        if p.startswith(("herdr-android/", "scripts/android-")):
            selected["android"] = True
        if p.startswith(("frontend/", "web/", "contracts/")):
            selected["frontend"] = True
        if p.startswith("web/"):
            selected["native"] = True
        if p.startswith(("relay/", "scripts/", ".pi/")) or p in ("install.sh", "Makefile") or (p.startswith("tests/") and p.endswith((".sh", ".mjs"))):
            selected["shell"] = True
        if p.startswith(("relay/", "deploy/native-tcp/", "scripts/package-native", "scripts/check-native")) or p in ("VERSION.json", "herdr-plugin.toml"):
            selected["native"] = True
        if p in ("VERSION.json", "Makefile") or p.startswith("contracts/"):
            selected["android"] = True
        if p == "Makefile" or p.startswith("contracts/"):
            selected["go"] = selected["frontend"] = True
        if p.startswith(("contracts/", "internal/protocol/", "internal/directterminal/", "internal/transport/", "internal/gateway/")) or p in ("internal/app/desktop_terminal.go", "internal/app/server.go"):
            selected["desktop"] = True
    return selected


def changed_paths(event, event_name):
    if event_name == "workflow_dispatch":
        return None
    if event_name == "pull_request":
        base = event["pull_request"]["base"]["sha"]
        head = event["pull_request"]["head"]["sha"]
        separator = "..."
    else:
        base, head = event.get("before", ""), event.get("after", os.getenv("GITHUB_SHA", ""))
        separator = ".."
    if not all(re.fullmatch(r"[0-9a-f]{40}", value or "") for value in (base, head)) or base == "0" * 40:
        return None
    try:
        raw = subprocess.check_output(["git", "diff", "--name-only", "--no-renames", "-z", base + separator + head, "--"], stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError:
        return None
    return [p.decode("utf-8", errors="surrogateescape") for p in raw.split(b"\0") if p]


def main():
    event = json.loads(Path(os.environ["GITHUB_EVENT_PATH"]).read_text(encoding="utf-8"))
    paths = changed_paths(event, os.environ["GITHUB_EVENT_NAME"])
    selected = dict.fromkeys(COMPONENTS, True) if paths is None else classify(paths)
    if os.environ["GITHUB_EVENT_NAME"] == "workflow_dispatch":
        component = event.get("inputs", {}).get("components", "all")
        if component in ("native", "android"):
            selected["native"] = component == "native"
            selected["android"] = component == "android"
    output = "".join(f"{key}={str(value).lower()}\n" for key, value in selected.items())
    with open(os.environ["GITHUB_OUTPUT"], "a", encoding="utf-8") as stream:
        stream.write(output)
    print("Selected: " + (", ".join(k for k, v in selected.items() if v) or "documentation only"))


if __name__ == "__main__":
    main()
