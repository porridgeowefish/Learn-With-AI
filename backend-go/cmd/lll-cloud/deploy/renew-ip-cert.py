#!/usr/bin/env python3
"""Renew the Learn-With-AI ingress IP certificate with the system OpenSSL and Python."""

import argparse
import base64
import fcntl
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
from urllib.error import HTTPError
from urllib.request import Request, urlopen


def encode(raw):
    return base64.urlsafe_b64encode(raw).rstrip(b"=").decode("ascii")


def json_bytes(value):
    return json.dumps(value, separators=(",", ":"), sort_keys=True).encode("utf-8")


def openssl(*args, data=None):
    result = subprocess.run(["openssl", *args], input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=20)
    if result.returncode:
        raise RuntimeError("OpenSSL " + args[0] + " failed: " + result.stderr.decode("utf-8", "replace").strip())
    return result.stdout


def ensure_key(path):
    if not path.exists():
        with tempfile.TemporaryDirectory(dir=path.parent) as work:
            temp_key = Path(work) / "key.pem"
            openssl("genpkey", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:2048",
                    "-pkeyopt", "rsa_keygen_pubexp:65537", "-out", str(temp_key))
            temp_key.chmod(0o600)
            temp_key.replace(path)


class OrderClient:
    def __init__(self, directory, key):
        with urlopen(directory, timeout=15) as response:
            self.directory = json.load(response)
        self.key = str(key)
        modulus = openssl("rsa", "-in", self.key, "-noout", "-modulus").decode().strip().split("=")[1]
        self.jwk = {"kty": "RSA", "n": encode(bytes.fromhex(modulus)), "e": "AQAB"}
        self.nonce = None
        self.account = None

    def signed(self, endpoint, payload):
        for _ in range(3):
            if self.nonce is None:
                request = Request(self.directory["newNonce"], method="HEAD")
                with urlopen(request, timeout=15) as response:
                    self.nonce = response.headers["Replay-Nonce"]
            protected = {"alg": "RS256", "nonce": self.nonce, "url": endpoint}
            self.nonce = None
            protected.update({"kid": self.account} if self.account else {"jwk": self.jwk})
            header = encode(json_bytes(protected))
            body = "" if payload is None else encode(json_bytes(payload))
            signature = openssl("dgst", "-sha256", "-sign", self.key, data=(header + "." + body).encode("ascii"))
            request = Request(endpoint, json_bytes({"protected": header, "payload": body, "signature": encode(signature)}),
                              headers={"Content-Type": "application/jose+json"}, method="POST")
            try:
                with urlopen(request, timeout=15) as response:
                    self.nonce = response.headers.get("Replay-Nonce")
                    return response.headers, response.read()
            except HTTPError as error:
                self.nonce = error.headers.get("Replay-Nonce")
                problem = json.load(error)
                if problem.get("type") != "urn:ietf:params:acme:error:badNonce":
                    raise RuntimeError("ACME rejected the request: " + str(problem.get("detail", error.code))) from None
        raise RuntimeError("ACME did not accept three consecutive nonces")

    def read(self, endpoint):
        return json.loads(self.signed(endpoint, None)[1])

    def wait(self, endpoint, pending, success):
        for _ in range(30):
            result = self.read(endpoint)
            state = result["status"]
            if state in success:
                return result
            if state not in pending:
                raise RuntimeError("ACME reached " + state + ": " + json.dumps(result.get("error", {})))
            time.sleep(2)
        raise TimeoutError("ACME polling limit reached")

    def issue(self, address, challenge_root, csr):
        self.account = self.signed(self.directory["newAccount"], {"termsOfServiceAgreed": True})[0]["Location"]
        headers, order = self.signed(self.directory["newOrder"], {
            "identifiers": [{"type": "ip", "value": address}], "profile": "shortlived",
        })
        location = headers["Location"]
        for auth_url in json.loads(order)["authorizations"]:
            auth = self.read(auth_url)
            if auth["identifier"] != {"type": "ip", "value": address}:
                raise RuntimeError("Unexpected ACME authorization identifier")
            if auth["status"] == "valid":
                continue
            challenge = next(item for item in auth["challenges"] if item["type"] == "http-01")
            token = challenge["token"]
            if not re.fullmatch(r"[A-Za-z0-9_-]{16,128}", token):
                raise RuntimeError("Unexpected ACME challenge token")
            proof = challenge_root / token
            thumbprint = encode(hashlib.sha256(json_bytes(self.jwk)).digest())
            with proof.open("x", encoding="ascii") as target:
                target.write(token + "." + thumbprint)
            try:
                proof.chmod(0o644)
                self.signed(challenge["url"], {})
                self.wait(auth_url, {"pending"}, {"valid"})
            finally:
                proof.unlink()
        order = self.wait(location, {"pending"}, {"ready"})
        self.signed(order["finalize"], {"csr": encode(csr)})
        order = self.wait(location, {"processing", "ready"}, {"valid"})
        return self.signed(order["certificate"], None)[1]


def renew(address, directory, state, challenge_root, reload_service):
    state.mkdir(mode=0o700, parents=True, exist_ok=True)
    with (state / "renew.lock").open("a") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        account_key, tls_key, certificate = (state / name for name in ("account.pem", "tls.key", "fullchain.pem"))
        reload_pending = state / "reload-pending"
        if reload_service and reload_pending.exists() and certificate.exists():
            subprocess.run(["systemctl", "reload", "lll-cloud-proxy"], check=True, timeout=15)
            reload_pending.unlink()
        if certificate.exists():
            probe = subprocess.run(["openssl", "x509", "-checkend", "259200", "-noout", "-in", str(certificate)],
                                   stdout=subprocess.DEVNULL, timeout=10)
            if probe.returncode == 0:
                return
        ensure_key(account_key)
        ensure_key(tls_key)
        csr = openssl("req", "-new", "-key", str(tls_key), "-subj", "/", "-addext",
                      "subjectAltName=IP:" + address, "-outform", "DER")
        chain = OrderClient(directory, account_key).issue(address, challenge_root, csr)
        with tempfile.TemporaryDirectory(dir=state) as work:
            temp_cert = Path(work) / "fullchain.pem"
            temp_cert.write_bytes(chain)
            openssl("x509", "-in", str(temp_cert), "-noout", "-checkip", address, "-checkend", "259200")
            public_key = openssl("x509", "-in", str(temp_cert), "-noout", "-pubkey")
            if public_key != openssl("pkey", "-in", str(tls_key), "-pubout"):
                raise RuntimeError("Certificate does not match the ingress key")
            if reload_service:
                reload_pending.touch()
            temp_cert.replace(certificate)
        if reload_service:
            subprocess.run(["systemctl", "reload", "lll-cloud-proxy"], check=True, timeout=15)
            reload_pending.unlink()
        print("Installed a renewed certificate for " + address)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ip", required=True)
    parser.add_argument("--staging", action="store_true")
    parser.add_argument("--reload", action="store_true")
    args = parser.parse_args()
    if args.staging and args.reload:
        parser.error("Staging certificates cannot reload the live ingress.")
    os.umask(0o077)
    address = str(ipaddress.IPv4Address(args.ip))
    ca = "acme-staging-v02" if args.staging else "acme-v02"
    state = Path("/etc/lll-cloud") / ("tls-staging" if args.staging else "tls")
    renew(address, f"https://{ca}.api.letsencrypt.org/directory", state,
          Path("/var/www/lll-cloud-acme/.well-known/acme-challenge"), args.reload)


if __name__ == "__main__":
    main()
