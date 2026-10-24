#!/usr/bin/env python3
"""Destructive authentication/bootstrap checks for an ISOLATED Compose CI project only.
No token, password, OTP or signed URL is printed. Not a production maintenance tool.
"""
import base64
import hashlib
import hmac
import http.client
import http.cookiejar
from html.parser import HTMLParser
import json
import os
from pathlib import Path
import secrets
import struct
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid

from bootstrap import Bootstrap

KC = "http://127.0.0.1:8180"
REALM = "/admin/realms/agrigreen"


def call(method, path, body=None, token=None, form=False):
    data = None if body is None else (urllib.parse.urlencode(body).encode() if form else json.dumps(body).encode())
    headers = {"Content-Type": "application/x-www-form-urlencoded" if form else "application/json"}
    if token:
        headers["Authorization"] = "Bearer " + (token() if callable(token) else token)
    request = urllib.request.Request(KC + path, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            raw = response.read()
            return response.status, json.loads(raw) if raw else None
    except urllib.error.HTTPError as error:
        try:
            code = json.load(error).get("error", "unknown")
        except ValueError:
            code = "unknown"
        raise RuntimeError(f"Keycloak {method} HTTP {error.code}: {code}") from None


def totp(secret):
    mac = hmac.new(secret.encode(), struct.pack(">Q", int(time.time()) // 30), hashlib.sha1).digest()
    offset = mac[-1] & 15
    return str((struct.unpack(">I", mac[offset:offset + 4])[0] & 0x7fffffff) % 1000000).zfill(6)


class Page(HTMLParser):
    def __init__(self, raw):
        super().__init__()
        self.action = None
        self.fields = {}
        self.reset = None
        self.links = []
        self.inputs = set()
        self.feed(raw)

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        if tag == "form":
            self.action = attrs.get("action")
        if tag == "input" and attrs.get("name"):
            self.inputs.add(attrs["name"])
        if tag == "a" and attrs.get("href"):
            self.links.append(attrs["href"])
        if tag == "input" and attrs.get("type") == "hidden":
            self.fields[attrs["name"]] = attrs.get("value", "")
        if tag == "a" and "reset-credentials" in attrs.get("href", ""):
            self.reset = attrs["href"]


class CompletedRedirect(Exception):
    """Keep the one-time code in memory; never expose it in exception text."""
    def __init__(self, target):
        super().__init__("OIDC callback captured (credential redacted)")
        parsed = urllib.parse.urlsplit(target)
        if parsed.scheme != "https" or parsed.netloc != "app.vps.example.test" or parsed.path != "/":
            raise RuntimeError("unexpected OIDC callback origin")
        fields = urllib.parse.parse_qs(parsed.query)
        if set(fields) & {"error", "error_description"} or len(fields.get("code", [])) != 1 or len(fields.get("state", [])) != 1:
            raise RuntimeError("OIDC callback lacks a unique code/state")
        self.code = fields["code"][0]
        self.state = fields["state"][0]


class StopAtApp(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, response, code, message, headers, target):
        parsed = urllib.parse.urlsplit(target)
        if parsed.hostname == "app.vps.example.test":
            raise CompletedRedirect(target)
        if parsed.scheme != "https" or parsed.netloc != "auth.vps.example.test":
            raise RuntimeError("unexpected browser redirect origin")
        return super().redirect_request(request, response, code, message, headers, target)


class LoopbackHTTPS(urllib.request.HTTPSHandler):
    # Model TLS termination at the VPS proxy while keeping logical HTTPS URLs
    # for Secure cookies. This test transport connects only to isolated Keycloak
    # on localhost; it makes no claim about external DNS/certificate acceptance.
    def https_open(self, request):
        if request.host != "auth.vps.example.test":
            raise RuntimeError("unexpected browser host in isolated smoke")
        return self.do_open(lambda *_args, **_kwargs: http.client.HTTPConnection("127.0.0.1", 8180, timeout=30), request)


def local(url):
    url = urllib.parse.urljoin("https://auth.vps.example.test/", url)
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "https" or parsed.hostname != "auth.vps.example.test":
        raise RuntimeError("unexpected browser URL in isolated smoke")
    return url


def claims(token):
    return json.loads(base64.urlsafe_b64decode(token.split(".")[1] + "=="))


class RecoveryBrowser:
    def __init__(self, realm, client):
        self.realm, self.client = realm, client
        self.verifier = secrets.token_urlsafe(48)
        self.challenge = base64.urlsafe_b64encode(hashlib.sha256(self.verifier.encode()).digest()).decode().rstrip("=")
        self.state = secrets.token_urlsafe(24)
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), LoopbackHTTPS(), StopAtApp(),
            urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def begin(self):
        query = urllib.parse.urlencode({"client_id": self.client, "redirect_uri": "https://app.vps.example.test/",
            "response_type": "code", "scope": "openid profile", "state": self.state,
            "code_challenge": self.challenge, "code_challenge_method": "S256"})
        return Page(self.opener.open("https://auth.vps.example.test/realms/" + self.realm +
            "/protocol/openid-connect/auth?" + query).read().decode())

    def post(self, page, **values):
        assert page.action, "expected an authentication form"
        fields = dict(page.fields, **values)
        return Page(self.opener.open(local(page.action), data=urllib.parse.urlencode(fields).encode()).read().decode())

    def exchange(self, completed):
        if completed.state != self.state:
            raise RuntimeError("OIDC callback state mismatch")
        return call("POST", "/realms/" + self.realm + "/protocol/openid-connect/token", {
            "grant_type": "authorization_code", "client_id": self.client, "code": completed.code,
            "redirect_uri": "https://app.vps.example.test/", "code_verifier": self.verifier}, form=True)[1]


def no_session(admin, root, uid):
    _, sessions = call("GET", root + "/users/" + uid + "/sessions", token=admin)
    assert sessions == [], "MFA incomplete but Keycloak created an authenticated user session"


def reset_email(browser, username):
    login_page = browser.begin()
    assert login_page.reset, "login must expose forgot-password link"
    reset = Page(browser.opener.open(local(login_page.reset)).read().decode())
    browser.post(reset, username=username)
    deadline = time.monotonic() + 30
    message_id = None
    while message_id is None and time.monotonic() < deadline:
        with urllib.request.urlopen("http://127.0.0.1:8025/api/v1/messages", timeout=15) as mail:
            mailbox = json.load(mail)
        message_id = next((message["ID"] for message in mailbox["messages"]
            if any(recipient["Address"] == username + "@example.test" for recipient in message["To"])), None)
        if message_id is None:
            time.sleep(0.2)
    assert message_id is not None, "reset email was not delivered to isolated Mailpit"
    with urllib.request.urlopen("http://127.0.0.1:8025/api/v1/message/" + message_id, timeout=15) as mail:
        captured = json.load(mail)
    link = next(link for link in Page(captured["HTML"]).links if "/login-actions/action-token" in link)
    # Reset links, page action URLs, codes and OTP secrets remain in memory.
    return Page(browser.opener.open(local(link)).read().decode())


def recovery_probe(admin, realm, client, configured):
    root = "/admin/realms/" + realm
    username = "p0-reset-" + secrets.token_hex(7)
    password, new_password = "Aa7#" + secrets.token_urlsafe(24), "Bb8#" + secrets.token_urlsafe(24)
    secret = secrets.token_urlsafe(24)
    credentials = [{"type": "password", "value": password, "temporary": False}]
    if configured:
        credentials.append({"type": "otp", "userLabel": "P0 disposable recovery",
            "secretData": json.dumps({"value": secret}),
            "credentialData": json.dumps({"subType": "totp", "digits": 6, "counter": 0, "period": 30, "algorithm": "HmacSHA1"})})
    call("POST", root + "/users", {"username": username, "enabled": True, "emailVerified": True,
        "email": username + "@example.test", "firstName": "Synthetic", "lastName": "Recovery",
        "attributes": {"tenant_id": [str(uuid.uuid4())]}, "credentials": credentials}, token=admin)
    _, users = call("GET", root + "/users?username=" + username + "&exact=true", token=admin)
    uid = users[0]["id"]
    try:
        role = "kn_user" if realm == "agrigreen" else "platform_admin"
        _, role_rep = call("GET", root + "/roles/" + role, token=admin)
        call("POST", root + "/users/" + uid + "/role-mappings/realm", [role_rep], token=admin)
        _, prior = call("GET", root + "/users/" + uid + "/credentials", token=admin)
        prior_otps = {c["id"] for c in prior if c["type"] == "otp"}
        browser = RecoveryBrowser(realm, client)
        try:
            page = reset_email(browser, username)
            no_session(admin, root, uid)
            if not configured:
                # Depending on required-action priority the password page may
                # precede CONFIGURE_TOTP, but no code/session may be issued.
                if {"password-new", "password-confirm"}.issubset(page.inputs):
                    page = browser.post(page, **{"password-new": new_password, "password-confirm": new_password})
                assert {"totp", "totpSecret"}.issubset(page.inputs), "required missing OTP must reach CONFIGURE_TOTP"
                assert "execution=CONFIGURE_TOTP" in page.action
                no_session(admin, root, uid)
                page = browser.post(page, totp="", userLabel="P0 incomplete")
                assert {"totp", "totpSecret"}.issubset(page.inputs)
                no_session(admin, root, uid)
                print("--- PASS: " + realm + " required missing OTP cannot finish email recovery or create a session; CONFIGURE_TOTP enforced")
                return
            assert "otp" in page.inputs and "password-new" not in page.inputs, "OTP must precede password reset"
            page = browser.post(page, otp="")
            assert "otp" in page.inputs
            no_session(admin, root, uid)
            page = browser.post(page, otp="invalid-otp")
            assert "otp" in page.inputs
            no_session(admin, root, uid)
            page = browser.post(page, otp=totp(secret))
            assert {"password-new", "password-confirm"}.issubset(page.inputs)
            no_session(admin, root, uid)
        except CompletedRedirect:
            raise AssertionError("MFA bypass: email recovery returned an authorization code before OTP") from None
        try:
            browser.post(page, **{"password-new": new_password, "password-confirm": new_password})
            raise AssertionError("password reset did not complete its authorization-code redirect")
        except CompletedRedirect as completed:
            token = browser.exchange(completed)["access_token"]
        assert "otp" in claims(token).get("amr", []), "reset authorization-code token lacks real OTP AMR"
        _, after = call("GET", root + "/users/" + uid + "/credentials", token=admin)
        assert {c["id"] for c in after if c["type"] == "otp"} == prior_otps
        call("POST", root + "/users/" + uid + "/logout", token=admin)
        # Check the old password through the actual browser for both realms;
        # the separate admin realm deliberately has no direct-grant client.
        old_browser = RecoveryBrowser(realm, client)
        old_page = old_browser.post(old_browser.begin(), username=username, password=password)
        assert {"username", "password"}.issubset(old_page.inputs) and "otp" not in old_page.inputs
        no_session(admin, root, uid)
        new_browser = RecoveryBrowser(realm, client)
        otp_page = new_browser.post(new_browser.begin(), username=username, password=new_password)
        assert "otp" in otp_page.inputs
        no_session(admin, root, uid)
        time.sleep(max(1, 31 - int(time.time()) % 30))
        try:
            new_browser.post(otp_page, otp=totp(secret))
            raise AssertionError("browser password+OTP did not return authorization code")
        except CompletedRedirect as completed:
            token = new_browser.exchange(completed)["access_token"]
        assert {"pwd", "otp"}.issubset(claims(token).get("amr", []))
        print("--- PASS: " + realm + " email reset requires OTP, exchanges real PKCE code with otp AMR, preserves credential and rejects old/password-only login")
    finally:
        call("DELETE", root + "/users/" + uid, token=admin)


def main():
    if os.environ.get("AG_P0_SMOKE_DISPOSABLE") != "1":
        raise RuntimeError("requires AG_P0_SMOKE_DISPOSABLE=1; isolated CI project only")
    # Generated by prepare.sh; no evaluation of shell code or value logging.
    values = {}
    for line in Path("deploy/compose/.env").read_text().splitlines():
        if "=" in line and not line.lstrip().startswith("#"):
            key, value = line.split("=", 1)
            values[key] = value.strip("'\"")
    if values.get("AG_DOMAIN") != "vps.example.test" or not os.environ.get("COMPOSE_PROJECT_NAME", "").startswith("agrigreen-p0-"):
        raise RuntimeError("refusing smoke outside the generated isolated Compose project/domain")
    _, session = call("POST", "/realms/master/protocol/openid-connect/token", {
        "grant_type": "password", "client_id": "admin-cli", "username": "admin",
        "password": values["KEYCLOAK_ADMIN_PASSWORD"]}, form=True)
    admin_expires = time.monotonic() + max(1, session.get("expires_in", 60) - 15)
    def admin():
        nonlocal session, admin_expires
        if time.monotonic() >= admin_expires:
            _, session = call("POST", "/realms/master/protocol/openid-connect/token", {
                "grant_type": "password", "client_id": "admin-cli", "username": "admin",
                "password": values["KEYCLOAK_ADMIN_PASSWORD"]}, form=True)
            admin_expires = time.monotonic() + max(1, session.get("expires_in", 60) - 15)
        return session["access_token"]
    _, policy = call("GET", REALM, token=admin)
    assert policy["permanentLockout"] is False and policy["failureFactor"] == 5
    assert policy["waitIncrementSeconds"] == 900 and policy["maxFailureWaitSeconds"] == 900
    assert policy["resetPasswordAllowed"] is True
    assert policy["resetCredentialsFlow"] == "agrigreen password recovery"
    _, recovery = call("GET", REALM + "/authentication/flows/agrigreen%20password%20recovery/executions", token=admin)
    assert {"reset-credentials-choose-user", "reset-credential-email", "reset-password", "auth-otp-form"}.issubset({step.get("providerId") for step in recovery})
    assert "reset-otp" not in {step.get("providerId") for step in recovery}
    print("--- PASS: imported production policy is temporary 15-minute lockout plus password reset")
    _, clients = call("GET", REALM + "/clients?clientId=agrigreen-seed", token=admin)
    client = clients[0]
    assert client["enabled"] is False
    call("PUT", REALM + "/clients/" + client["id"], {"enabled": True}, token=admin)
    username = "p0-smoke-" + secrets.token_hex(6)
    password = secrets.token_urlsafe(24) + "#7"
    otp_secret = secrets.token_urlsafe(24)
    tenant = str(uuid.uuid4())
    uid = None
    try:
        call("POST", REALM + "/users", {
            "username": username, "enabled": True, "emailVerified": True,
            "email": username + "@example.test", "firstName": "Synthetic", "lastName": "P0",
            "attributes": {"tenant_id": [tenant]},
            "credentials": [
                {"type": "password", "value": password, "temporary": False},
                {"type": "otp", "userLabel": "P0 disposable",
                 "secretData": json.dumps({"value": otp_secret}),
                 "credentialData": json.dumps({"subType": "totp", "digits": 6, "counter": 0, "period": 30, "algorithm": "HmacSHA1"})}
            ]}, token=admin)
        _, users = call("GET", REALM + "/users?username=" + username + "&exact=true", token=admin)
        uid = users[0]["id"]
        roles = []
        for role in ["platform_admin", "masterdata_admin", "registry_admin", "geo_admin"]:
            _, role_rep = call("GET", REALM + "/roles/" + role, token=admin)
            roles.append(role_rep)
        call("POST", REALM + "/users/" + uid + "/role-mappings/realm", roles, token=admin)

        def login(pwd=password, with_otp=True):
            fields = {"grant_type": "password", "client_id": "agrigreen-seed", "username": username,
                      "password": pwd, "scope": "openid profile"}
            if with_otp:
                fields.update({"totp": totp(otp_secret), "otp": totp(otp_secret)})
            return call("POST", "/realms/agrigreen/protocol/openid-connect/token", fields, form=True)[1]

        token = login()["access_token"]
        payload = json.loads(base64.urlsafe_b64decode(token.split(".")[1] + "=="))
        assert {"pwd", "otp"}.issubset(payload.get("amr", []))
        assert payload["tenant_id"] == tenant
        assert payload["aud"] == "agrigreen-api" or "agrigreen-api" in payload["aud"]
        print("--- PASS: real password+TOTP produces signed token with pwd/otp AMR and accepts profile scope")
        # The platform verifies this real issuer token, including MFA, during bootstrap.
        fixture = {"type": "FeatureCollection", "features": [{"type": "Feature", "properties": {"code": "30292"},
            "geometry": {"type": "Polygon", "coordinates": [[[105.30, 10.10], [105.31, 10.10], [105.31, 10.11], [105.30, 10.11], [105.30, 10.10]]]}}]}
        result = Bootstrap("http://127.0.0.1:8080", "http://127.0.0.1:8082", token, "SMOKE").run(
            "91", fixture, "SYNTHETIC CI ONLY - never authoritative boundaries")
        assert result["commune_boundaries"] == 1 and len(result["team_refs"]) == 1
        print("--- PASS: real Admin APIs bootstrap catalogs, organization tree and explicitly synthetic commune geometry")
        # Exercise expiry using a short timeout only inside this disposable realm,
        # then restore the exact 900s production policy in finally.
        call("PUT", REALM, {"waitIncrementSeconds": 2, "maxFailureWaitSeconds": 2,
             "minimumQuickLoginWaitSeconds": 1, "quickLoginCheckMilliSeconds": 1}, token=admin)
        for _ in range(5):
            try:
                login("incorrect-" + secrets.token_hex(8))
                raise AssertionError("wrong password accepted")
            except RuntimeError as error:
                assert "HTTP 400" in str(error) or "HTTP 401" in str(error)
        time.sleep(0.5)
        _, failure = call("GET", REALM + "/attack-detection/brute-force/users/" + uid, token=admin)
        assert failure.get("numFailures", 0) >= 5
        _, user = call("GET", REALM + "/users/" + uid, token=admin)
        assert user["enabled"] is True
        # Use a fresh timestep even when Keycloak rejects TOTP reuse.
        time.sleep(max(3, 31 - int(time.time()) % 30))
        login()
        print("--- PASS: real brute-force lock expires; login resumes without administrator unlock (2s isolated policy probe)")
        for realm, web_client in [("agrigreen", "agrigreen-web-ops"), ("agrigreen-kn-admin", "agrigreen-kn-admin-web")]:
            recovery_probe(admin, realm, web_client, configured=True)
            recovery_probe(admin, realm, web_client, configured=False)
    finally:
        call("PUT", REALM, {key: policy[key] for key in ["waitIncrementSeconds", "maxFailureWaitSeconds", "minimumQuickLoginWaitSeconds", "quickLoginCheckMilliSeconds"]}, token=admin)
        call("PUT", REALM + "/clients/" + client["id"], {"enabled": False}, token=admin)
        if uid:
            call("DELETE", REALM + "/users/" + uid, token=admin)


if __name__ == "__main__":
    main()
