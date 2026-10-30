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
    async def discover(port=None, api_key=None):
        calls.append(api_key)
        if api_key == "invalid-inbox-key":
            raise ValueError("provider rejected secret")
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
