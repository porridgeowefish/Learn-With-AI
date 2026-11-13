"""Forward permitted OAuth HTTPS requests through configured workstation routes."""

import json
import logging
import os
import select
import socket
import sys
import threading
import time
from logging.handlers import RotatingFileHandler
from pathlib import Path
from urllib.parse import urlsplit

import paramiko


ALLOWED = {b"oauth2.googleapis.com:443", b"openidconnect.googleapis.com:443", b"github.com:443", b"api.github.com:443"}
MAX_CONNECTIONS = threading.BoundedSemaphore(32)


def proxy_addresses(explicit):
    values = list(explicit)
    values += [os.environ.get("HTTPS_PROXY", "")]
    if sys.platform == "win32":
        import winreg
        try:
            with winreg.OpenKey(winreg.HKEY_CURRENT_USER,
                                r"Software\Microsoft\Windows\CurrentVersion\Internet Settings") as key:
                if winreg.QueryValueEx(key, "ProxyEnable")[0]:
                    setting = winreg.QueryValueEx(key, "ProxyServer")[0]
                    if "=" in setting:
                        setting = next((part.split("=", 1)[1] for part in setting.split(";")
                                        if part.strip().lower().startswith("https=")), "")
                    values.append(setting)
        except OSError:
            pass
    result = []
    for value in values:
        if not value:
            continue
        try:
            parsed = urlsplit(value if "://" in value else "http://" + value)
            if (parsed.scheme == "http" and parsed.hostname in {"127.0.0.1", "::1", "localhost"}
                    and parsed.port and not (parsed.username or parsed.password or parsed.path or parsed.query or parsed.fragment)):
                address = (parsed.hostname, parsed.port)
                if address not in result:
                    result.append(address)
        except ValueError:
            continue
    return result


def open_tunnel(target, explicit):
    host = target.rsplit(b":", 1)[0].decode("ascii")
    for address in [*proxy_addresses(explicit), None]:
        connection = None
        try:
            connection = socket.create_connection(address or (host, 443), timeout=3)
            connection.settimeout(3)
            if address:
                connection.sendall(b"CONNECT " + target + b" HTTP/1.1\r\nHost: " + target + b"\r\n\r\n")
                reply = bytearray()
                while b"\r\n\r\n" not in reply and len(reply) < 8192:
                    chunk = connection.recv(4096)
                    if not chunk:
                        break
                    reply.extend(chunk)
                fields = bytes(reply).split(b"\r\n", 1)[0].split()
                if b"\r\n\r\n" not in reply or len(fields) < 2 or fields[1] != b"200":
                    raise OSError("proxy CONNECT rejected")
            return connection
        except OSError:
            if connection:
                connection.close()
    return None


def relay(channel, proxy_urls):
    with channel:
        channel.settimeout(5)
        header = bytearray()
        while b"\r\n\r\n" not in header:
            chunk = channel.recv(4096)
            if not chunk:
                return
            header.extend(chunk)
            if len(header) > 8192:
                return
        fields = bytes(header).split(b"\r\n", 1)[0].split()
        if len(fields) != 3 or fields[0] != b"CONNECT" or fields[1] not in ALLOWED:
            channel.sendall(b"HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n")
            return
        tunnel = open_tunnel(fields[1], proxy_urls)
        if tunnel is None:
            channel.sendall(b"HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\n\r\n")
            return
        with tunnel:
            tunnel.settimeout(5)
            channel.sendall(b"HTTP/1.1 200 Connection Established\r\n\r\n")
            while True:
                readers, _, _ = select.select([channel, tunnel], [], [], 25)
                if not readers:
                    return
                for source in readers:
                    data = source.recv(32768)
                    if not data:
                        return
                    (tunnel if source is channel else channel).sendall(data)


def accept(channel, _origin, _server, proxy_urls):
    if not MAX_CONNECTIONS.acquire(blocking=False):
        channel.close()
        return

    def run():
        try:
            relay(channel, proxy_urls)
        except (OSError, EOFError, paramiko.SSHException):
            pass
        finally:
            MAX_CONNECTIONS.release()

    threading.Thread(target=run, daemon=True).start()


def check_connection(transport):
    received = threading.Event()

    def heartbeat():
        try:
            # OpenSSH replies even when it declines the keepalive request.
            transport.global_request("keepalive@openssh.com", wait=True)
            received.set()
        except (OSError, EOFError, paramiko.SSHException):
            pass

    threading.Thread(target=heartbeat, daemon=True).start()
    return received.wait(8) and transport.is_active()


def forward(transport, ports, proxy_urls):
    def handler(channel, origin, server):
        accept(channel, origin, server, proxy_urls)

    for port in ports:
        try:
            transport.request_port_forward("127.0.0.1", port, handler=handler)
            return port
        except paramiko.SSHException:
            logging.warning("OAuth relay could not bind remote port %d", port)
    raise paramiko.SSHException("No available OAuth relay port")


def serve(host, user, ports, directory, proxy_urls):
    backoff = 2
    while True:
        try:
            with paramiko.SSHClient() as ssh:
                ssh.load_host_keys(str(directory / "known_hosts"))
                ssh.connect(
                    host, username=user, key_filename=str(directory / "id_ed25519"),
                    allow_agent=False, look_for_keys=False, timeout=8,
                    auth_timeout=8, banner_timeout=8,
                )
                transport = ssh.get_transport()
                port = forward(transport, ports, proxy_urls)
                logging.info("OAuth relay connected on remote port %d", port)
                backoff = 2
                while transport.is_active():
                    time.sleep(15)
                    if not check_connection(transport):
                        break
        except (OSError, EOFError, paramiko.SSHException) as error:
            logging.warning("Google OAuth relay reconnecting after %s", type(error).__name__)
        time.sleep(backoff)
        backoff = min(backoff * 2, 30)


if __name__ == "__main__":
    target, username, remote_port, data_dir = sys.argv[1:]
    directory = Path(data_dir)
    settings_path = directory / "relay-settings.json"
    settings = json.loads(settings_path.read_text(encoding="utf-8")) if settings_path.is_file() else {}
    ports = tuple(dict.fromkeys((int(remote_port), *settings.get("fallbackRemotePorts", []))))
    urls = settings.get("localProxyURLs", [])
    if len(ports) > 2 or any(type(port) is not int or not 0 < port < 65536 for port in ports):
        raise ValueError("expected at most two valid relay ports")
    if not isinstance(urls, list) or len(urls) > 2 or not all(isinstance(url, str) for url in urls):
        raise ValueError("expected at most two local proxy URLs")
    logging.basicConfig(
        level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s",
        handlers=[RotatingFileHandler(directory / "relay.log", maxBytes=131072, backupCount=2)],
    )
    serve(target, username, ports, directory, urls)
