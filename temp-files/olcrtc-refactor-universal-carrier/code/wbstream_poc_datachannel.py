#!/usr/bin/env python3
"""PoC: WB Stream DataChannel over LiveKit."""

import asyncio
import base64
import json
import logging
import requests

try:
    from livekit import rtc
except ImportError:
    print("[!] Error: livekit library not installed.\nRun: pip install livekit requests")
    exit(1)

logging.getLogger("livekit").setLevel(logging.WARNING)

API_BASE = "https://stream.wb.ru"
WS_URL = "wss://rtc-el-01.wb.ru"
HARDCODED_ROOM_ID = "019e23c2-a580-7550-b08a-7ac5342ca21f"
TEST_ATTEMPTS = 60
TEST_MESSAGES = [f"WB Stream DataChannel attempt {idx:02d}" for idx in range(1, TEST_ATTEMPTS + 1)]


def _decode_jwt_payload(token: str) -> dict:
    """Decode JWT payload without verifying the signature; useful for inspecting LiveKit grants."""
    try:
        payload = token.split(".")[1]
        payload += "=" * (-len(payload) % 4)
        return json.loads(base64.urlsafe_b64decode(payload))
    except Exception as exc:
        return {"decode_error": str(exc)}


def _print_token_grants(label: str, token: str) -> None:
    payload = _decode_jwt_payload(token)
    print(f"     {label} token identity={payload.get('sub')} name={payload.get('name')}")
    print(f"     {label} video grants={json.dumps(payload.get('video', {}), ensure_ascii=False, sort_keys=True)}")

def _get_room_token(room_id: str, display_name: str) -> tuple[str, str]:
    """Retrieves the room token via the guest API."""
    headers = {
        "User-Agent": "Mozilla/5.0 (Linux x86_64)",
        "Content-Type": "application/json"
    }

    reg_req = requests.post(
        f"{API_BASE}/auth/api/v1/auth/user/guest-register",
        json={"displayName": display_name, "device": {"deviceName": "Linux", "deviceType": "PARTICIPANT_DEVICE_TYPE_WEB_DESKTOP"}},
        headers=headers
    )
    reg_req.raise_for_status()
    headers["Authorization"] = f"Bearer {reg_req.json()['accessToken']}"

    if not room_id:
        room_req = requests.post(f"{API_BASE}/api-room/api/v2/room", json={"roomType": "ROOM_TYPE_ALL_ON_SCREEN", "roomPrivacy": "ROOM_PRIVACY_FREE"}, headers=headers)
        room_req.raise_for_status()
        room_id = room_req.json()["roomId"]

    requests.post(f"{API_BASE}/api-room/api/v1/room/{room_id}/join", json={}, headers=headers).raise_for_status()
    tok_req = requests.get(f"{API_BASE}/api-room-manager/v2/room/{room_id}/connection-details", params={"deviceType": "PARTICIPANT_DEVICE_TYPE_WEB_DESKTOP", "displayName": display_name}, headers=headers)
    tok_req.raise_for_status()
    return room_id, tok_req.json()["roomToken"]
async def run_poc() -> dict:
    """Runs the complete PoC flow."""
    print("\n--- WB Stream PoC ---")
    results = {
        "server_ok": False,
        "client_ok": False,
        "sent": 0,
        "server_recv": 0,
        "echo_sent": 0,
        "client_recv": 0,
        "errors": [],
    }
    
    server, client = rtc.Room(), rtc.Room()
    shared_room_id = HARDCODED_ROOM_ID

    print("[1/3] Connecting Server & Client...")
    try:
        shared_room_id, server_tok = _get_room_token(shared_room_id, "OlcRTC-Server")
        _, client_tok = _get_room_token(shared_room_id, "OlcRTC-Client")
        _print_token_grants("server", server_tok)
        _print_token_grants("client", client_tok)

        @server.on("data_received")
        def on_server_data(dp: rtc.DataPacket):
            if dp.topic == "olcrtc":
                msg = dp.data.decode(errors="replace")
                results["server_recv"] += 1
                print(f" <- Server recv #{results['server_recv']:02d}: {msg}")

                async def echo() -> None:
                    try:
                        await server.local_participant.publish_data(f"Echo: {msg}".encode(), topic="olcrtc")
                        results["echo_sent"] += 1
                    except Exception as exc:
                        results["errors"].append(f"Echo failed: {exc}")

                asyncio.create_task(echo())

        @client.on("data_received")
        def on_client_data(dp: rtc.DataPacket):
            if dp.topic == "olcrtc":
                results["client_recv"] += 1
                print(f" <- Client recv #{results['client_recv']:02d}: {dp.data.decode(errors='replace')}")

        await server.connect(WS_URL, server_tok)
        results["server_ok"] = True
        await client.connect(WS_URL, client_tok)
        results["client_ok"] = True
        print(f" :P Peers connected to room: {shared_room_id}")
    except Exception as e:
        results["errors"].append(str(e))
        return results

    print("\n[2/3] Exchanging messages...")
    await asyncio.sleep(1)
    
    for idx, msg in enumerate(TEST_MESSAGES, 1):
        try:
            await client.local_participant.publish_data(msg.encode(), topic="olcrtc")
            results["sent"] += 1
            print(f" -> Sent: {msg}")
            await asyncio.sleep(0.5)
        except Exception as e:
            results["errors"].append(f"Sending {idx} failed: {str(e)}")

    await asyncio.sleep(2)
    
    print("\n[3/3] Cleaning up...")
    await server.disconnect()
    await client.disconnect()
    
    return results

def print_results(res: dict):
    print("\n--- TEST RESULTS ---")
    print(f"Server: {':P' if res['server_ok'] else 'X'} / Client: {':P' if res['client_ok'] else 'X'}")
    print(
        "Messages: "
        f"Client sent {res['sent']} / Server recv {res['server_recv']} / "
        f"Echo sent {res['echo_sent']} / Client recv {res['client_recv']}"
    )
    if res['errors']:
        for e in res['errors']: print(f" Error: {e}")
    print(f"\n{':P SUCCESS' if res['sent'] and res['sent'] == res['client_recv'] else 'X FAILED'}\n")

if __name__ == "__main__":
    try:
        res = asyncio.run(run_poc())
        print_results(res)
    except KeyboardInterrupt:
        pass
