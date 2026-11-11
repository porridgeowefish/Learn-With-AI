"""Forward the permitted OAuth HTTPS requests through the Windows Clash listener."""

import logging
import select
import socket
import sys
import threading
import time
from logging.handlers import RotatingFileHandler
from pathlib import Path

import paramiko


ALLOWED = {b"oauth2.googleapis.com:443", b"openidconnect.googleapis.com:443", b"github.com:443"}
MAX_CONNECTIONS = threading.BoundedSemaphore(32)


def relay(channel):
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
        with socket.create_connection(("127.0.0.1", 7890), timeout=5) as clash:
            clash.settimeout(5)
            # Rebuild the CONNECT request so callers cannot supply proxy headers.
            target = fields[1]
            clash.sendall(b"CONNECT " + target + b" HTTP/1.1\r\nHost: " + target + b"\r\n\r\n")
            while True:
                readers, _, _ = select.select([channel, clash], [], [], 25)
                if not readers:
                    return
                for source in readers:
                    data = source.recv(32768)
                    if not data:
                        return
                    (clash if source is channel else channel).sendall(data)


def accept(channel, _origin, _server):
    if not MAX_CONNECTIONS.acquire(blocking=False):
        channel.close()
        return

    def run():
        try:
            relay(channel)
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


def serve(host, user, port, directory):
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
                transport.request_port_forward("127.0.0.1", port, handler=accept)
                logging.info("Google OAuth relay connected")
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
    logging.basicConfig(
        level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s",
        handlers=[RotatingFileHandler(directory / "relay.log", maxBytes=131072, backupCount=2)],
    )
    serve(target, username, int(remote_port), directory)
