#!/usr/bin/env python3
import argparse
import json
import subprocess
import sys
import time


def docker_inspect(container_id: str):
    try:
        output = subprocess.check_output([
            "docker",
            "inspect",
            container_id,
        ], stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError:
        return None
    try:
        data = json.loads(output.decode("utf-8"))
    except json.JSONDecodeError:
        return None
    if not data:
        return None
    return data[0]


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--out-events", required=True)
    parser.add_argument("--out-inspects", required=True)
    parser.add_argument("--label", required=True)
    args = parser.parse_args()

    with open(args.out_events, "w", encoding="utf-8") as events_fp, open(
        args.out_inspects, "w", encoding="utf-8"
    ) as inspects_fp:
        proc = subprocess.Popen(
            [
                "docker",
                "events",
                "--format",
                "{{json .}}",
                "--filter",
                "type=container",
                "--filter",
                f"label={args.label}",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        event_index = 0
        try:
            if proc.stdout is None:
                raise RuntimeError("failed to open docker events stream")
            for line in proc.stdout:
                line = line.strip()
                if not line:
                    continue
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue

                events_fp.write(line + "\n")
                events_fp.flush()

                container_id = event.get("Actor", {}).get("ID", "")
                action = event.get("Action", "")
                if container_id:
                    inspect = docker_inspect(container_id)
                    if inspect is not None:
                        record = {
                            "event_index": event_index,
                            "timeNano": event.get("timeNano"),
                            "id": container_id,
                            "action": action,
                            "inspect": inspect,
                        }
                        inspects_fp.write(json.dumps(record) + "\n")
                        inspects_fp.flush()

                event_index += 1
        except KeyboardInterrupt:
            pass
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=2)
            except subprocess.TimeoutExpired:
                proc.kill()


if __name__ == "__main__":
    main()
