#!/usr/bin/env python3
import argparse
import json
import os
import signal
import subprocess
import sys
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Optional

import tomllib


@dataclass
class InspectRecord:
    event_index: int
    timeNano: Optional[int]
    id: str
    action: str
    inspect: Dict[str, Any]


class EventRecorder:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._cond = threading.Condition(self._lock)
        self.events: List[Dict[str, Any]] = []
        self.inspects: List[InspectRecord] = []

    def append(self, event: Dict[str, Any], inspect: Optional[Dict[str, Any]]) -> int:
        with self._cond:
            index = len(self.events)
            self.events.append(event)
            if inspect is not None:
                self.inspects.append(
                    InspectRecord(
                        event_index=index,
                        timeNano=event.get("timeNano"),
                        id=event.get("Actor", {}).get("ID", ""),
                        action=event.get("Action", ""),
                        inspect=inspect,
                    )
                )
            self._cond.notify_all()
            return index

    def wait_for(self, name: str, actions: List[str], start_index: int, timeout: float) -> int:
        deadline = time.time() + timeout
        name = name.lstrip("/")
        action_set = {a.lower() for a in actions}
        with self._cond:
            while True:
                for idx in range(start_index, len(self.events)):
                    event = self.events[idx]
                    actor = event.get("Actor", {})
                    actor_name = actor.get("Attributes", {}).get("name", "").lstrip("/")
                    if actor_name != name:
                        continue
                    action = event.get("Action", "").lower()
                    if action in action_set:
                        return idx + 1
                remaining = deadline - time.time()
                if remaining <= 0:
                    raise TimeoutError(f"timeout waiting for {name} actions {actions}")
                self._cond.wait(timeout=remaining)


def run_cmd(args: List[str], check: bool = True) -> subprocess.CompletedProcess:
    return subprocess.run(args, check=check, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)


def docker_inspect(container_id: str) -> Optional[Dict[str, Any]]:
    try:
        output = subprocess.check_output(["docker", "inspect", container_id], stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError:
        return None
    try:
        data = json.loads(output.decode("utf-8"))
    except json.JSONDecodeError:
        return None
    if not data:
        return None
    return data[0]


def cleanup_labeled(label: str) -> None:
    result = run_cmd(["docker", "ps", "-a", "-q", "--filter", f"label={label}"], check=False)
    ids = [line.strip() for line in result.stdout.splitlines() if line.strip()]
    if not ids:
        return
    run_cmd(["docker", "rm", "-f"] + ids, check=False)


def normalize_command(command: Any) -> List[str]:
    if command is None:
        return []
    if isinstance(command, list):
        return [str(item) for item in command]
    return [str(command)]


def start_event_stream(label: str, recorder: EventRecorder, stop_event: threading.Event) -> subprocess.Popen:
    proc = subprocess.Popen(
        [
            "docker",
            "events",
            "--format",
            "{{json .}}",
            "--filter",
            "type=container",
            "--filter",
            f"label={label}",
        ],
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        bufsize=1,
    )

    def reader() -> None:
        if proc.stdout is None:
            return
        for line in proc.stdout:
            if stop_event.is_set():
                break
            line = line.strip()
            if not line:
                continue
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue
            container_id = event.get("Actor", {}).get("ID", "")
            inspect = docker_inspect(container_id) if container_id else None
            recorder.append(event, inspect)

    thread = threading.Thread(target=reader, daemon=True)
    thread.start()
    return proc


def wait_for_actions(recorder: EventRecorder, name: str, actions: List[str], cursor: int) -> int:
    return recorder.wait_for(name, actions, cursor, timeout=10.0)


def run_action(step: Dict[str, Any], scenario_label: str, recorder: EventRecorder, cursor: int) -> int:
    action = str(step.get("action", "")).lower()
    if action == "sleep":
        time.sleep(float(step.get("seconds", 1)))
        return cursor

    if action == "run":
        name = step["name"]
        image = step["image"]
        cmd = ["docker", "run", "-d", "--name", name, "--label", "healthmon.test=1", "--label", scenario_label]
        for label in step.get("labels", []) or []:
            cmd.extend(["--label", str(label)])
        cmd.append(image)
        cmd.extend(normalize_command(step.get("command")))
        run_cmd(cmd)
        cursor = wait_for_actions(recorder, name, ["create"], cursor)
        cursor = wait_for_actions(recorder, name, ["start"], cursor)
        return cursor

    if action == "start":
        name = step["name"]
        run_cmd(["docker", "start", name])
        return wait_for_actions(recorder, name, ["start"], cursor)

    if action == "stop":
        name = step["name"]
        args = ["docker", "stop", name]
        run_cmd(args)
        return wait_for_actions(recorder, name, ["stop", "die"], cursor)

    if action == "kill":
        name = step["name"]
        signal_name = str(step.get("signal", "9"))
        run_cmd(["docker", "kill", "--signal", signal_name, name])
        return wait_for_actions(recorder, name, ["kill"], cursor)

    if action == "restart":
        name = step["name"]
        run_cmd(["docker", "restart", name])
        return wait_for_actions(recorder, name, ["restart", "start"], cursor)

    if action == "rename":
        old = step["from"]
        new = step["to"]
        run_cmd(["docker", "rename", old, new])
        return wait_for_actions(recorder, new, ["rename"], cursor)

    if action == "rm":
        name = step["name"]
        args = ["docker", "rm"]
        if step.get("force", True):
            args.append("-f")
        args.append(name)
        run_cmd(args)
        return wait_for_actions(recorder, name, ["destroy", "remove", "rm"], cursor)

    raise ValueError(f"unsupported action: {action}")


def load_scenario(path: Path) -> List[Dict[str, Any]]:
    with path.open("rb") as handle:
        data = tomllib.load(handle)
    steps = data.get("step")
    if not isinstance(steps, list):
        raise ValueError(f"scenario {path} must define [[step]] entries")
    return steps


def write_jsonl(path: Path, items: List[Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8") as handle:
        for item in items:
            handle.write(json.dumps(item) + "\n")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--scenario-dir", default="testdata/scenarios")
    parser.add_argument("--dump-dir", default="testdata/dumps")
    args = parser.parse_args()

    scenario_dir = Path(args.scenario_dir)
    dump_dir = Path(args.dump_dir)
    scenarios = sorted(scenario_dir.glob("*.toml"))
    if not scenarios:
        print(f"no scenarios found in {scenario_dir}", file=sys.stderr)
        return 1

    if not shutil_which("docker"):
        print("docker is required", file=sys.stderr)
        return 1

    stop_event = threading.Event()

    def handle_signal(_sig, _frame):
        stop_event.set()

    signal.signal(signal.SIGINT, handle_signal)
    signal.signal(signal.SIGTERM, handle_signal)

    for scenario_path in scenarios:
        if stop_event.is_set():
            break
        scenario_name = scenario_path.stem
        scenario_label = f"healthmon.scenario={scenario_name}"
        print(f"running scenario {scenario_name}")

        cleanup_labeled("healthmon.test=1")

        recorder = EventRecorder()
        proc = start_event_stream("healthmon.test=1", recorder, stop_event)
        cursor = 0
        try:
            steps = load_scenario(scenario_path)
            for step in steps:
                if stop_event.is_set():
                    break
                cursor = run_action(step, scenario_label, recorder, cursor)
        finally:
            stop_event.set()
            if proc.poll() is None:
                proc.terminate()
                try:
                    proc.wait(timeout=2)
                except subprocess.TimeoutExpired:
                    proc.kill()
            cleanup_labeled("healthmon.test=1")
            stop_event.clear()

        events_path = dump_dir / f"{scenario_name}.events.jsonl"
        inspects_path = dump_dir / f"{scenario_name}.inspects.jsonl"
        write_jsonl(events_path, recorder.events)
        write_jsonl(inspects_path, [record.__dict__ for record in recorder.inspects])
        print(f"wrote {events_path} and {inspects_path}")

    return 0


def shutil_which(cmd: str) -> Optional[str]:
    path = os.environ.get("PATH", "")
    for entry in path.split(os.pathsep):
        candidate = Path(entry) / cmd
        if candidate.exists() and os.access(candidate, os.X_OK):
            return str(candidate)
    return None


if __name__ == "__main__":
    raise SystemExit(main())
