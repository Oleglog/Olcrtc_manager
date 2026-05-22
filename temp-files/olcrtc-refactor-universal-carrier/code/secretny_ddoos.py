#!/usr/bin/env python3
import asyncio
import re
import time

import requests

API_BASE = "https://stream.wb.ru"
OUTPUT_FILE = "/tmp/ti_ymresh_ot_spida.txt"
HITS_FILE = "/tmp/ti_ymresh_v_mukah.txt"

PATTERNS = [
    re.compile(r"dead", re.IGNORECASE),
    re.compile(r"beef", re.IGNORECASE),
    re.compile(r"deadbeef", re.IGNORECASE),
]

CONCURRENCY = 50
TOTAL_ATTEMPTS = 0
PRINT_EVERY = 100


def _create_room_sync(idx: int) -> str | None:
    headers = {
        "User-Agent": "Mozilla/5.0 (Linux x86_64)",
        "Content-Type": "application/json",
    }
    try:
        reg = requests.post(
            f"{API_BASE}/auth/api/v1/auth/user/guest-register",
            json={
                "displayName": f"OlcRTC-DDoos-{idx}",
                "device": {
                    "deviceName": "Linux",
                    "deviceType": "PARTICIPANT_DEVICE_TYPE_WEB_DESKTOP",
                },
            },
            headers=headers,
            timeout=15,
        )
        reg.raise_for_status()
        headers["Authorization"] = f"Bearer {reg.json()['accessToken']}"

        room_req = requests.post(
            f"{API_BASE}/api-room/api/v2/room",
            json={
                "roomType": "ROOM_TYPE_ALL_ON_SCREEN",
                "roomPrivacy": "ROOM_PRIVACY_FREE",
            },
            headers=headers,
            timeout=15,
        )
        room_req.raise_for_status()
        return room_req.json()["roomId"]
    except Exception:
        return None


def _check_hit(room_id: str) -> str | None:
    best = None
    for p in PATTERNS:
        if p.search(room_id):
            if p.pattern.lower() == "deadbeef":
                return "DEADBEEF-JP"
            best = p.pattern
    return best


class Stats:
    __slots__ = ("attempts", "ok", "fail", "hits", "started")

    def __init__(self) -> None:
        self.attempts = 0
        self.ok = 0
        self.fail = 0
        self.hits = 0
        self.started = time.time()


async def worker(sem: asyncio.Semaphore, stats: Stats, idx: int) -> None:
    async with sem:
        loop = asyncio.get_running_loop()
        room_id = await loop.run_in_executor(None, _create_room_sync, idx)
        stats.attempts += 1

        if not room_id:
            stats.fail += 1
        else:
            stats.ok += 1
            with open(OUTPUT_FILE, "a", encoding="utf-8") as f:
                f.write(room_id + "\n")

            hit = _check_hit(room_id)
            if hit:
                stats.hits += 1
                line = f"[{hit}] {room_id}"
                print(f"\n!!! HIT !!! {line}\n")
                with open(HITS_FILE, "a", encoding="utf-8") as f:
                    f.write(line + "\n")

        if stats.attempts % PRINT_EVERY == 0:
            elapsed = time.time() - stats.started
            rps = stats.attempts / elapsed if elapsed else 0
            print(
                f"[{stats.attempts}] ok={stats.ok} fail={stats.fail} "
                f"hits={stats.hits} rps={rps:.1f}"
            )


async def main() -> None:
    print("--- imba: DEADBEEF ---")
    print(f"all room   -> {OUTPUT_FILE}")
    print(f"heat dead/beef-> {HITS_FILE}")
    print(f"parralel   : {CONCURRENCY}")
    print(f"limit : {'∞' if TOTAL_ATTEMPTS == 0 else TOTAL_ATTEMPTS}")
    print()

    sem = asyncio.Semaphore(CONCURRENCY)
    stats = Stats()
    idx = 0

    try:
        if TOTAL_ATTEMPTS > 0:
            tasks = [
                asyncio.create_task(worker(sem, stats, i))
                for i in range(1, TOTAL_ATTEMPTS + 1)
            ]
            await asyncio.gather(*tasks)
        else:
            running: set[asyncio.Task] = set()
            while True:
                while len(running) < CONCURRENCY * 2:
                    idx += 1
                    running.add(asyncio.create_task(worker(sem, stats, idx)))
                done, running = await asyncio.wait(
                    running, return_when=asyncio.FIRST_COMPLETED
                )
    except KeyboardInterrupt:
        pass
    finally:
        elapsed = time.time() - stats.started
        print("\n--- ITOGY ---")
        print(f"runs : {stats.attempts}")
        print(f"OK      : {stats.ok}")
        print(f"FAIL    : {stats.fail}")
        print(f"heatS   : {stats.hits}")
        print(f"timw   : {elapsed:.1f}s")


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        pass
