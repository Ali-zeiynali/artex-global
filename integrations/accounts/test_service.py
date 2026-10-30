import importlib.util
import json
import time
from pathlib import Path
from types import SimpleNamespace

import pytest
from fastapi.testclient import TestClient
from telethon import errors


@pytest.fixture
def broker(tmp_path, monkeypatch):
    monkeypatch.setenv("ACCOUNTS_HOME", str(tmp_path / "private"))
    monkeypatch.setenv("ACCOUNTS_BRIDGE_TOKEN", "test-bridge-token")
    spec = importlib.util.spec_from_file_location("accounts_test_service", Path(__file__).with_name("service.py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.INTEGRATIONS = tmp_path / "integrations"
    module.SKILLS = tmp_path / "skills"
    module.SKILLS.mkdir()
    async def restore(): pass
    async def provision(aid):
        module.update(aid, status="connected", tool_count=100)
        return module.public(module.account(aid))
    module._real_provision = module.provision
    monkeypatch.setattr(module, "restore", restore)
    monkeypatch.setattr(module, "provision", provision)
    with TestClient(module.app, headers={"Authorization": "Bearer test-bridge-token"}) as client:
        yield module, client
    module.DB.close()


class FakeTelegram:
    def __init__(self, *args, **kwargs): self.disconnected = False
    async def connect(self): pass
    async def disconnect(self): self.disconnected = True
    async def send_code_request(self, phone): return SimpleNamespace(phone_code_hash="private-code-hash", type=SimpleNamespace())
    async def sign_in(self, *args, **kwargs):
        if "password" in kwargs:
            if kwargs["password"] != "correct-two-step": raise errors.PasswordHashInvalidError(request=None)
        else:
            if args[1] != "12345": raise errors.PhoneCodeInvalidError(request=None)
            raise errors.SessionPasswordNeededError(request=None)
    async def get_me(self): return SimpleNamespace(phone="15551234567", first_name="Test", last_name="Account")


def test_phone_otp_two_factor_flow_never_returns_credentials(broker, monkeypatch):
    module, client = broker
    monkeypatch.setattr(module, "TelegramClient", FakeTelegram)
    r = client.post("/v1/accounts/telegram/start", json={"phone": "+15551234567", "api_id": 1234, "api_hash": "a" * 32})
    assert r.status_code == 200
    cid = r.json()["challenge_id"]
    assert client.post("/v1/accounts/telegram/verify", json={"challenge_id": cid, "code": "00000"}).status_code == 400
    assert client.post("/v1/accounts/telegram/verify", json={"challenge_id": cid, "code": "12345"}).json()["step"] == "password"
    assert client.post("/v1/accounts/telegram/password", json={"challenge_id": cid, "password": "wrong"}).status_code == 400
    r = client.post("/v1/accounts/telegram/password", json={"challenge_id": cid, "password": "correct-two-step"})
    assert r.status_code == 200 and r.json()["step"] == "connected"
    result = client.get("/v1/accounts").json()
    assert result["telegram_app_configured"] is True
    assert result["accounts"][0]["identity"] == "+15551234567"
    payload = json.dumps(result)
    assert "api_hash" not in payload and "secrets" not in payload and "correct-two-step" not in payload
    assert b'"api_hash"' not in (module.ROOT / "accounts.sqlite").read_bytes()
    assert cid not in module.CHALLENGES


def test_unauthorized_and_expired_login(broker, monkeypatch):
    module, client = broker
    assert client.get("/v1/accounts", headers={"Authorization": "Bearer wrong"}).status_code == 401
    assert client.post("/v1/accounts/telegram/verify", json={"challenge_id": "missing", "code": "12345"}).status_code == 410
    monkeypatch.setattr(module, "TelegramClient", FakeTelegram)
    cid = client.post("/v1/accounts/telegram/start", json={"phone": "+15551234567", "api_id": 123, "api_hash": "b" * 32}).json()["challenge_id"]
    module.CHALLENGES[cid]["expires"] = time.time() - 1
    assert client.post("/v1/accounts/telegram/verify", json={"challenge_id": cid, "code": "12345"}).status_code == 410
    assert client.post("/v1/accounts/telegram/cancel", json={"challenge_id": cid}).status_code == 200
    assert cid not in module.CHALLENGES


def test_gmail_multi_account_and_secret_redaction(broker, monkeypatch):
    module, client = broker
    async def validate(*_args): pass
    monkeypatch.setattr(module, "validate_gmail", validate)
    for email in ["first@gmail.com", "second@gmail.com"]:
        r = client.post("/v1/accounts/gmail", json={"email": email, "password": "test appp pass word", "label": email})
        assert r.status_code == 200
    assert client.post("/v1/accounts/gmail", json={"email": "first@gmail.com", "password": "testappppassword"}).status_code == 409
    result = client.get("/v1/accounts").json()
    assert len(result["accounts"]) == 2
    assert "testappppassword" not in json.dumps(result)
    assert b"testappppassword" not in (module.ROOT / "accounts.sqlite").read_bytes()
    assert "no-store" in client.get("/v1/accounts").headers["cache-control"]


def test_telegram_without_two_step_finishes_after_code(broker, monkeypatch):
    module, client = broker
    class NoPasswordTelegram(FakeTelegram):
        async def sign_in(self, *args, **kwargs): pass
    monkeypatch.setattr(module, "TelegramClient", NoPasswordTelegram)
    cid = client.post("/v1/accounts/telegram/start", json={"phone": "+15551234567", "api_id": 123, "api_hash": "c" * 32}).json()["challenge_id"]
    result = client.post("/v1/accounts/telegram/verify", json={"challenge_id": cid, "code": "12345"})
    assert result.status_code == 200 and result.json()["step"] == "connected"


def test_atomic_multi_account_auth_and_secret_redaction(broker, monkeypatch):
    module, client = broker
    calls = []
    async def discover(port=None, api_key=None, expected_identity=None):
        calls.append(api_key)
        if api_key == "invalid-inbox-key":
            raise module.HTTPException(400, "Provider rejected the inbox key")
        return [{"name": "help", "inputSchema": {"type": "object"}}]
    monkeypatch.setattr(module, "discover", discover)
    assert client.post("/v1/accounts/atomicmail", json={"email": "bad", "api_key": "short"}).status_code == 400
    bad = client.post("/v1/accounts/atomicmail", json={"email": "first@atomicmail.ai", "api_key": "invalid-inbox-key"})
    assert bad.status_code == 400 and "invalid-inbox-key" not in bad.text
    for index in range(2):
        key = "test-inbox-secret-" + str(index)
        r = client.post("/v1/accounts/atomicmail", json={"email": f"inbox{index}@atomicmail.ai", "api_key": key})
        assert r.status_code == 200 and key not in r.text
        assert key.encode() not in (module.ROOT / "accounts.sqlite").read_bytes()
    assert len(client.get("/v1/accounts").json()["accounts"]) == 2
    assert client.post("/v1/accounts/atomicmail", json={"email": "inbox0@atomicmail.ai", "api_key": "test-inbox-secret-0"}).status_code == 409


def test_telegram_avatar_is_private_and_missing_photo_falls_back(broker):
    import asyncio
    module, client = broker
    aid = module.create("telegram", "Photo test", "+15550001111", {"api_id": 123, "api_hash": "a" * 32})
    path = f"/v1/accounts/{aid}/avatar"
    assert client.get(path).status_code == 404
    class PhotoClient:
        async def download_profile_photo(self, entity, **kwargs):
            assert entity == "me" and kwargs["download_big"] is False
            return b"\xff\xd8\xffprivate-profile-photo"
    asyncio.run(module.cache_avatar(PhotoClient(), aid))
    assert client.get(path, headers={"Authorization": "Bearer invalid"}).status_code == 401
    r = client.get(path)
    assert r.status_code == 200 and r.headers["content-type"] == "image/jpeg"
    assert r.headers["cache-control"] == "no-store"
    assert client.get("/v1/accounts").json()["accounts"][0]["avatar_version"]
    class NoPhotoClient:
        async def download_profile_photo(self, *_args, **_kwargs): return None
    asyncio.run(module.cache_avatar(NoPhotoClient(), aid))
    assert client.get(path).status_code == 404
    assert client.get("/v1/accounts").json()["accounts"][0]["avatar_version"] is None


def test_atomic_requires_real_inbox_auth_and_matching_address(broker, monkeypatch):
    import httpx
    module, client = broker
    calls = []
    def handler(request):
        rpc = json.loads(request.content)
        calls.append(rpc["method"])
        method = rpc["method"]
        if method == "notifications/initialized": return httpx.Response(202)
        if method == "initialize": result = {"protocolVersion": "2025-06-18", "capabilities": {}, "serverInfo": {"name": "test", "version": "1"}}
        elif method == "tools/list": result = {"tools": [{"name": "list_agents", "inputSchema": {"type": "object"}}]}
        elif method == "tools/call":
            # Simulate a vendor which lists tools for a bad key but denies the
            # actual authenticated operation, even with HTTP 200.
            if request.headers["authorization"] != "Bearer valid-test-inbox-key":
                result = {"isError": True, "content": [{"type": "text", "text": "authorization failed"}]}
            else:
                result = {"content": [{"type": "text", "text": json.dumps({"agents": [{"accountId": "inbox-one", "email": "real@atomicmail.ai"}]})}]}
        else: return httpx.Response(204)
        return httpx.Response(200, json={"jsonrpc": "2.0", "id": rpc.get("id"), "result": result})
    upstream = httpx.AsyncClient(transport=httpx.MockTransport(handler))
    monkeypatch.setattr(module, "HTTP", upstream)
    for key, email in [("invalid-test-inbox-key", "real@atomicmail.ai"), ("valid-test-inbox-key", "wrong@atomicmail.ai")]:
        response = client.post("/v1/accounts/atomicmail", json={"email": email, "api_key": key})
        assert response.status_code == 400
        assert key not in response.text
        assert not client.get("/v1/accounts").json()["accounts"]
    response = client.post("/v1/accounts/atomicmail", json={"email": "real@atomicmail.ai", "api_key": "valid-test-inbox-key"})
    assert response.status_code == 200
    assert calls.count("tools/call") == 3


def test_atomic_proxy_decodes_gzip_and_preserves_private_auth(broker, monkeypatch):
    import gzip
    import httpx
    module, client = broker
    aid = module.create("atomicmail", "Compressed upstream", "test@atomicmail.ai", {"api_key": "private-vendor-inbox-key"})
    mcp_token = module.credentials(module.account(aid))["mcp_token"]
    result = {"jsonrpc": "2.0", "id": 1, "result": {"tools": [{"name": "help"}]}}
    compressed = gzip.compress(json.dumps(result).encode())
    class WireStream(httpx.AsyncByteStream):
        async def __aiter__(self):
            yield compressed[:12]
            yield compressed[12:]
    def handler(request):
        assert request.headers["authorization"] == "Bearer private-vendor-inbox-key"
        assert "cookie" not in request.headers
        return httpx.Response(200, headers={"Content-Encoding": "gzip", "Content-Type": "application/json", "Mcp-Session-Id": "test-session"}, stream=WireStream())
    monkeypatch.setattr(module, "HTTP", httpx.AsyncClient(transport=httpx.MockTransport(handler)))
    response = client.post(f"/mcp/{aid}", headers={"Authorization": "Bearer " + mcp_token, "Cookie": "browser=private"}, json={"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
    assert response.status_code == 200 and response.json() == result
    assert "content-encoding" not in response.headers
    assert response.headers["mcp-session-id"] == "test-session"
    assert "private-vendor-inbox-key" not in response.text


def test_account_not_connected_until_artex_discovers_tools(broker, monkeypatch):
    module, client = broker
    aid = module.create("atomicmail", "Catalog check", "test@atomicmail.ai", {"api_key": "valid-test-inbox-key"})
    async def discover(**kwargs): return [{"name": "help"}, {"name": "list_agents"}]
    events = []
    async def artex(path, body=None, method=None):
        events.append((path, body))
        if path == "/api/mcp": return {"id": 123}
        if path.endswith("/refresh"): raise module.HTTPException(502, "Transport failed")
        raise AssertionError(path)
    monkeypatch.setattr(module, "discover", discover)
    monkeypatch.setattr(module, "artex", artex)
    monkeypatch.setattr(module, "provision", module._real_provision)
    response = client.post(f"/v1/accounts/{aid}/test")
    assert response.status_code == 502
    assert module.account(aid)["status"] == "error"
    assert any(path == "/api/mcp" and body.get("enabled") is False for path, body in events)
    assert module.account(aid)["mcp_name"] not in (module.SKILLS / "atomicmail-accounts/SKILL.md").read_text()
