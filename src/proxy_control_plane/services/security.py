from __future__ import annotations

import base64
import hashlib
import hmac
import json
import secrets
from datetime import UTC, datetime, timedelta


def _b64encode(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).decode().rstrip("=")


def _b64decode(data: str) -> bytes:
    padding = "=" * (-len(data) % 4)
    return base64.urlsafe_b64decode(data + padding)


def password_hash(password: str, iterations: int = 260_000) -> str:
    salt = secrets.token_hex(16)
    digest = hashlib.pbkdf2_hmac("sha256", password.encode(), salt.encode(), iterations)
    return f"pbkdf2_sha256${iterations}${salt}${digest.hex()}"


def verify_password(password: str, stored: str) -> bool:
    if stored.startswith("pbkdf2_sha256$"):
        _, iterations_text, salt, expected = stored.split("$", 3)
        digest = hashlib.pbkdf2_hmac(
            "sha256",
            password.encode(),
            salt.encode(),
            int(iterations_text),
        ).hex()
        return hmac.compare_digest(digest, expected)

    # Initial admin password can be supplied as plain env text for bootstrap.
    return hmac.compare_digest(password, stored)


def create_access_token(subject: str, secret_key: str, expires_minutes: int) -> str:
    header = {"alg": "HS256", "typ": "JWT"}
    payload = {
        "sub": subject,
        "exp": int((datetime.now(UTC) + timedelta(minutes=expires_minutes)).timestamp()),
    }
    signing_input = ".".join(
        [
            _b64encode(json.dumps(header, separators=(",", ":")).encode()),
            _b64encode(json.dumps(payload, separators=(",", ":")).encode()),
        ]
    )
    signature = hmac.new(secret_key.encode(), signing_input.encode(), hashlib.sha256).digest()
    return f"{signing_input}.{_b64encode(signature)}"


def verify_access_token(token: str, secret_key: str) -> str | None:
    try:
        header, payload, signature = token.split(".", 2)
        signing_input = f"{header}.{payload}"
        expected = hmac.new(secret_key.encode(), signing_input.encode(), hashlib.sha256).digest()
        if not hmac.compare_digest(_b64decode(signature), expected):
            return None
        payload_data = json.loads(_b64decode(payload))
        if int(payload_data["exp"]) < int(datetime.now(UTC).timestamp()):
            return None
        return str(payload_data["sub"])
    except Exception:
        return None


def new_subscription_token() -> str:
    return secrets.token_urlsafe(32)


def token_digest(token: str) -> str:
    return hashlib.sha256(token.encode()).hexdigest()

