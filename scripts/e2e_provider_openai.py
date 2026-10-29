#!/usr/bin/env python3
"""Provider Key Sharing OpenAI path e2e against local Crynux AS."""

from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Any

from eth_account import Account
from eth_account.messages import encode_defunct

AS_BASE = "http://127.0.0.1:8090"
PROVIDER_MODEL = "gpt-5.6-sol"
def mock_api_key() -> str:
    return f"sk-mock-e2e-openai-key-{int(time.time())}"


@dataclass
class Wallet:
    name: str
    address: str
    key: Account


def http_json(
    method: str,
    url: str,
    body: dict[str, Any] | None = None,
    headers: dict[str, str] | None = None,
    timeout: float = 120.0,
) -> tuple[int, Any]:
    data = None
    req_headers = {"Accept": "application/json"}
    if headers:
        req_headers.update(headers)
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        req_headers["Content-Type"] = "application/json"
    req = urllib.request.Request(url, data=data, headers=req_headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read().decode("utf-8")
            payload = json.loads(raw) if raw else None
            return resp.status, payload
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8")
        try:
            payload = json.loads(raw) if raw else None
        except json.JSONDecodeError:
            payload = raw
        return exc.code, payload


def login(wallet: Wallet) -> str:
    ts = int(time.time())
    message = f"Crynux AS\nAction: Login\nAddress: {wallet.address}\nTimestamp: {ts}"
    signed = wallet.key.sign_message(encode_defunct(text=message))
    status, payload = http_json(
        "POST",
        f"{AS_BASE}/v1/auth/login",
        {
            "address": wallet.address,
            "timestamp": ts,
            "signature": signed.signature.hex(),
        },
    )
    if status != 200:
        raise RuntimeError(f"login failed for {wallet.name}: {status} {payload}")
    token = payload["data"]["token"]
    print(f"[ok] login {wallet.name} {wallet.address}")
    return token


def create_project(jwt: str, name: str) -> tuple[str, str]:
    status, payload = http_json(
        "POST",
        f"{AS_BASE}/v1/projects",
        {"name": name},
        {"Authorization": f"Bearer {jwt}"},
    )
    if status != 200:
        raise RuntimeError(f"create project failed: {status} {payload}")
    data = payload["data"]
    print(f"[ok] project endpoint_token={data['endpoint_token'][:12]}...")
    return data["endpoint_token"], data["api_key"]


def delete_existing_keys(jwt: str) -> None:
    status, payload = http_json(
        "GET",
        f"{AS_BASE}/v1/earn/keys",
        headers={"Authorization": f"Bearer {jwt}"},
    )
    if status != 200:
        raise RuntimeError(f"list keys failed: {status} {payload}")
    items = payload.get("data", [])
    if isinstance(items, dict):
        items = items.get("keys", [])
    for item in items:
        key_id = item.get("id")
        if not key_id:
            continue
        del_status, del_payload = http_json(
            "DELETE",
            f"{AS_BASE}/v1/earn/keys/{key_id}",
            headers={"Authorization": f"Bearer {jwt}"},
        )
        if del_status != 200:
            raise RuntimeError(f"delete key {key_id} failed: {del_status} {del_payload}")
        print(f"[ok] deleted existing key id={key_id}")


def upload_openai_key(jwt: str) -> str:
    delete_existing_keys(jwt)
    status, payload = http_json(
        "POST",
        f"{AS_BASE}/v1/earn/keys",
        {"service_provider": "openai", "api_key": mock_api_key()},
        {"Authorization": f"Bearer {jwt}"},
    )
    if status != 200:
        raise RuntimeError(f"upload key failed: {status} {payload}")
    key_id = payload["data"]["id"]
    print(f"[ok] uploaded openai key id={key_id}")
    return key_id


def earn_summary(jwt: str) -> dict[str, Any]:
    status, payload = http_json(
        "GET",
        f"{AS_BASE}/v1/earn",
        headers={"Authorization": f"Bearer {jwt}"},
    )
    if status != 200:
        raise RuntimeError(f"earn summary failed: {status} {payload}")
    return payload["data"]


def project_get(method: str, endpoint: str, api_key: str, path: str, body: dict[str, Any] | None = None) -> tuple[int, Any]:
    return http_json(
        method,
        f"{AS_BASE}/api/{endpoint}/v1{path}",
        body,
        {"Authorization": f"Bearer {api_key}"},
    )


def project_stream(endpoint: str, api_key: str, path: str, body: dict[str, Any]) -> str:
    data = json.dumps(body).encode("utf-8")
    req = urllib.request.Request(
        f"{AS_BASE}/api/{endpoint}/v1{path}",
        data=data,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
            "Accept": "text/event-stream",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=120) as resp:
        return resp.read().decode("utf-8")


def assert_true(cond: bool, msg: str) -> None:
    if not cond:
        raise AssertionError(msg)


def run_matrix(endpoint: str, api_key: str) -> None:
    status, models = project_get("GET", endpoint, api_key, "/models")
    assert_true(status == 200, f"models status={status}")
    ids = [m.get("id") for m in models.get("data", [])]
    assert_true(PROVIDER_MODEL in ids, f"provider model missing in {ids}")
    owned = next(m for m in models["data"] if m["id"] == PROVIDER_MODEL)
    assert_true(owned.get("owned_by") == "openai", f"owned_by={owned}")
    print("[ok] GET /models includes gpt-5.6-sol")

    status, chat = project_get(
        "POST",
        endpoint,
        api_key,
        "/chat/completions",
        {
            "model": PROVIDER_MODEL,
            "messages": [{"role": "user", "content": "hi"}],
            "stream": False,
            "max_tokens": 32,
        },
    )
    assert_true(status == 200, f"chat non-stream status={status} body={chat}")
    assert_true(chat["choices"][0]["message"]["content"] == "mock-ok", f"chat content={chat}")
    print("[ok] chat completions non-stream")

    sse = project_stream(
        endpoint,
        api_key,
        "/chat/completions",
        {
            "model": PROVIDER_MODEL,
            "messages": [{"role": "user", "content": "hi"}],
            "stream": True,
            "max_tokens": 32,
        },
    )
    assert_true("mock-ok" in sse, f"stream missing content: {sse[:300]}")
    assert_true("[DONE]" in sse, "stream missing [DONE]")
    print("[ok] chat completions stream")

    status, tools = project_get(
        "POST",
        endpoint,
        api_key,
        "/chat/completions",
        {
            "model": PROVIDER_MODEL,
            "messages": [{"role": "user", "content": "use tool"}],
            "tools": [
                {
                    "type": "function",
                    "function": {
                        "name": "lookup",
                        "description": "lookup",
                        "parameters": {"type": "object", "properties": {"q": {"type": "string"}}},
                    },
                }
            ],
            "tool_choice": "auto",
            "max_tokens": 32,
        },
    )
    assert_true(status == 200, f"tools status={status} body={tools}")
    print("[ok] chat completions with tools")

    status, fmt = project_get(
        "POST",
        endpoint,
        api_key,
        "/chat/completions",
        {
            "model": PROVIDER_MODEL,
            "messages": [{"role": "user", "content": "json please"}],
            "response_format": {"type": "json_object"},
            "max_tokens": 32,
        },
    )
    assert_true(status == 200, f"json_object status={status} body={fmt}")
    print("[ok] chat completions response_format=json_object")

    status, resp = project_get(
        "POST",
        endpoint,
        api_key,
        "/responses",
        {
            "model": PROVIDER_MODEL,
            "input": "hi",
            "stream": False,
            "max_output_tokens": 32,
        },
    )
    assert_true(status == 200, f"responses non-stream status={status} body={resp}")
    print("[ok] responses non-stream")

    sse_resp = project_stream(
        endpoint,
        api_key,
        "/responses",
        {
            "model": PROVIDER_MODEL,
            "input": "hi",
            "stream": True,
            "max_output_tokens": 32,
        },
    )
    assert_true("mock-ok" in sse_resp or "response.completed" in sse_resp, f"responses stream unexpected: {sse_resp[:400]}")
    print("[ok] responses stream")

    status, bad = project_get(
        "POST",
        endpoint,
        api_key,
        "/completions",
        {"model": PROVIDER_MODEL, "prompt": "hi", "max_tokens": 8},
    )
    assert_true(status == 400, f"completions expected 400 got {status} {bad}")
    print("[ok] completions rejected for provider model")


def prepare_and_withdraw(jwt: str, amount_wei: str, network: str, wallet: Wallet) -> dict[str, Any]:
    status, prep = http_json(
        "POST",
        f"{AS_BASE}/v1/earn/withdrawals/prepare",
        {"amount": amount_wei, "network": network},
        {"Authorization": f"Bearer {jwt}"},
    )
    if status != 200:
        raise RuntimeError(f"prepare withdraw failed: {status} {prep}")
    data = prep["data"]
    message = data["message"]
    signed = wallet.key.sign_message(encode_defunct(text=message))
    status, created = http_json(
        "POST",
        f"{AS_BASE}/v1/earn/withdrawals",
        {
            "amount": data["amount"],
            "network": data["network"],
            "benefit_address": data["benefit_address"],
            "timestamp": data["timestamp"],
            "signature": "0x" + signed.signature.hex() if not signed.signature.hex().startswith("0x") else signed.signature.hex(),
        },
        {"Authorization": f"Bearer {jwt}"},
    )
    if status != 200:
        raise RuntimeError(f"create withdraw failed: {status} {created}")
    print(f"[ok] withdrawal created id={created['data']['id']} amount={created['data']['amount']}")
    return created["data"]


def wait_withdrawal_success(jwt: str, withdrawal_id: int, timeout_s: int = 180) -> dict[str, Any]:
    deadline = time.time() + timeout_s
    while time.time() < deadline:
        status, payload = http_json(
            "GET",
            f"{AS_BASE}/v1/earn/withdrawals?limit=20",
            headers={"Authorization": f"Bearer {jwt}"},
        )
        if status != 200:
            raise RuntimeError(f"list withdrawals failed: {status} {payload}")
        data = payload.get("data") or {}
        items = data.get("items") if isinstance(data, dict) else data
        for row in items or []:
            if int(row["id"]) == int(withdrawal_id):
                print(f"[info] withdrawal {withdrawal_id} status={row.get('status')} tx={row.get('tx_hash')}")
                if row.get("status") == "success":
                    return row
                if row.get("status") in {"failed", "rejected"}:
                    raise RuntimeError(f"withdrawal ended poorly: {row}")
        time.sleep(5)
    raise TimeoutError(f"withdrawal {withdrawal_id} not success within {timeout_s}s")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--mode", choices=["setup-matrix", "withdraw", "all"], default="all")
    parser.add_argument("--privkey-a", required=True)
    parser.add_argument("--privkey-b", required=True)
    parser.add_argument("--network", default="crynux-on-base-sepolia")
    parser.add_argument("--state-file", default="scripts/.e2e_provider_openai_state.json")
    args = parser.parse_args()

    wallet_a = Wallet("A", Account.from_key(args.privkey_a).address, Account.from_key(args.privkey_a))
    wallet_b = Wallet("B", Account.from_key(args.privkey_b).address, Account.from_key(args.privkey_b))

    if args.mode in ("setup-matrix", "all"):
        jwt_a = login(wallet_a)
        jwt_b = login(wallet_b)
        endpoint, api_key = create_project(jwt_a, f"e2e-provider-{int(time.time())}")
        upload_openai_key(jwt_b)
        before = earn_summary(jwt_b)
        print(f"[info] earn before={before}")
        run_matrix(endpoint, api_key)
        after = earn_summary(jwt_b)
        print(f"[info] earn after={after}")
        available_before = int(before.get("available", "0"))
        available_after = int(after.get("available", "0"))
        assert_true(available_after > available_before, f"earn did not increase: {before} -> {after}")
        state = {
            "endpoint_token": endpoint,
            "api_key": api_key,
            "jwt_b": jwt_b,
            "address_a": wallet_a.address,
            "address_b": wallet_b.address,
            "available": str(available_after),
        }
        with open(args.state_file, "w", encoding="utf-8") as f:
            json.dump(state, f, indent=2)
        print(f"[ok] state written to {args.state_file}")

    if args.mode in ("withdraw", "all"):
        with open(args.state_file, encoding="utf-8") as f:
            state = json.load(f)
        jwt_b = login(wallet_b)
        summary = earn_summary(jwt_b)
        available = summary.get("available", "0")
        print(f"[info] withdraw available={available}")
        assert_true(int(available) > 0, "no available earnings to withdraw")
        created = prepare_and_withdraw(jwt_b, available, args.network, wallet_b)
        final = wait_withdrawal_success(jwt_b, created["id"])
        print(f"[ok] withdrawal success tx_hash={final.get('tx_hash')}")
        end_summary = earn_summary(jwt_b)
        print(f"[info] earn final={end_summary}")

    print("[done] e2e_provider_openai passed")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # noqa: BLE001
        print(f"[fail] {exc}", file=sys.stderr)
        raise
