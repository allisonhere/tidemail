#!/usr/bin/env python3
"""Exercise the built CLI through a real controlling terminal and local SMTP fixture."""
import argparse
import fcntl
import json
import os
import pathlib
import pty
import select
import sqlite3
import socket
import struct
import subprocess
import tempfile
import termios
import threading
import time


class Session:
    def __init__(self, binary, args, env, cwd, piped=None, json_output=False):
        self.master, slave = pty.openpty()
        self.slave = slave
        self.before = termios.tcgetattr(slave)
        self.resize(80, 24)

        def controlling_terminal():
            os.setsid()
            fcntl.ioctl(slave, termios.TIOCSCTTY, 0)

        self.process = subprocess.Popen(
            [binary, *args], stdin=subprocess.PIPE if piped is not None else slave,
            stdout=subprocess.PIPE if json_output else slave, stderr=slave,
            env=env, cwd=cwd, preexec_fn=controlling_terminal, pass_fds=(slave,),
        )
        if piped is not None:
            self.process.stdin.write(piped.encode())
            self.process.stdin.close()
        self.transcript = ""
        self.position = 0

    def resize(self, width, height):
        fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))

    def send(self, keys):
        os.write(self.master, keys.encode())

    def expect(self, text):
        deadline = time.monotonic() + 8
        while time.monotonic() < deadline:
            index = self.transcript.find(text, self.position)
            if index >= 0:
                self.position = index + len(text)
                return
            if select.select([self.master], [], [], 0.1)[0]:
                self.transcript += os.read(self.master, 65536).decode(errors="replace")
        self.process.kill()
        self.process.wait()
        os.close(self.master)
        os.close(self.slave)
        raise AssertionError(f"Expected {text!r}; terminal output:\n{self.transcript[-5000:]}")

    def finish(self):
        try:
            code = self.process.wait(timeout=8)
            assert code == 0, (code, self.transcript[-3000:])
            after = termios.tcgetattr(self.slave)
            assert after == self.before, "terminal settings were not restored"
            output = self.process.stdout.read() if self.process.stdout else b""
            if output:
                assert b"\x1b" not in output, "terminal escapes contaminated stdout"
            return output
        finally:
            if self.process.poll() is None:
                self.process.kill()
                self.process.wait()
            os.close(self.master)
            os.close(self.slave)


class SMTPFixture:
    def __init__(self):
        self.listener = socket.socket()
        self.listener.bind(("127.0.0.1", 0))
        self.listener.listen()
        self.listener.settimeout(120)
        self.port = self.listener.getsockname()[1]
        self.message = b""
        self.error = None
        self.thread = threading.Thread(target=self.serve, daemon=True)
        self.thread.start()

    def serve(self):
        try:
            with self.listener.accept()[0] as conn:
                conn.settimeout(10)
                stream = conn.makefile("rwb", buffering=0)
                stream.write(b"220 localhost fixture\r\n")
                while True:
                    line = stream.readline()
                    if not line:
                        return
                    command = line.split(b" ", 1)[0].strip().upper()
                    if command in (b"EHLO", b"HELO"):
                        stream.write(b"250-localhost\r\n250 AUTH PLAIN\r\n")
                    elif command == b"AUTH":
                        stream.write(b"235 authenticated\r\n")
                    elif command == b"DATA":
                        stream.write(b"354 send data\r\n")
                        while True:
                            data = stream.readline()
                            if data == b".\r\n":
                                break
                            if not data:
                                raise AssertionError("SMTP disconnected inside DATA")
                            self.message += data
                        stream.write(b"250 accepted\r\n")
                    elif command == b"QUIT":
                        stream.write(b"221 goodbye\r\n")
                        return
                    else:
                        stream.write(b"250 ok\r\n")
        except Exception as error:
            self.error = error
        finally:
            self.listener.close()


def compose_to_review(session, attach=False):
    session.expect("Sender account")
    session.send("\r")
    session.expect("2/9  To")
    session.send("you@example.com\r")
    session.expect("CC (optional)")
    session.send("\r")
    session.expect("BCC (optional)")
    session.send("\r")
    session.expect("5/9  Subject")
    session.send("Fixture report\r")
    session.expect("Message body")
    session.send("Hello from the wizard\rSecond line\x13")
    session.expect("7/9  Signature")
    session.send("\r")
    session.expect("8/9  Attachments")
    if attach:
        session.send("a")
        session.expect("Pick an attachment")
        session.expect("report.txt")
        session.send("\r")
        session.expect("8/9  Attachments")
        session.expect("report.txt")
    session.send("\r")
    session.expect("9/9  Review")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", nargs="?", default="/tmp/tidemail-agent-cli")
    args = parser.parse_args()
    binary = str(pathlib.Path(args.binary).resolve())
    with tempfile.TemporaryDirectory(prefix="tidemail-wizard-") as directory:
        root = pathlib.Path(directory)
        config_dir = root / "config" / "tidemail"
        config_dir.mkdir(parents=True)
        work = root / "work"
        work.mkdir()
        (work / "report.txt").write_text("Approved attachment bytes")
        env = dict(os.environ, HOME=directory, XDG_CONFIG_HOME=str(root / "config"),
                   XDG_DATA_HOME=str(root / "data"), TERM="xterm-256color", NO_COLOR="1")
        fixture = SMTPFixture()
        config_path = config_dir / "config.toml"
        config_text = f'''[[account]]
id = "wizard-fixture"
name = "Wizard fixture"
user = "me@example.com"
from = "me@example.com"
smtp_host = "127.0.0.1"
smtp_port = {fixture.port}
smtp_tls = false
signature = "Fixture signature"
[display]
send_delay_seconds = 3600
'''
        config_path.write_text(config_text)

        menu = Session(binary, ["cli"], env, work)
        menu.expect("Choose a command")
        menu.send("q")
        menu.finish()
        print("PASS command menu and terminal restoration")

        draft = Session(binary, ["send"], env, work)
        compose_to_review(draft, attach=True)
        draft.resize(35, 12)
        time.sleep(0.1)
        draft.resize(80, 24)
        # The selected bytes must remain stable after a file changes on disk.
        (work / "report.txt").write_text("Different bytes after review")
        draft.send("\r")
        draft.expect("drafted")
        draft.finish()
        with sqlite3.connect(root / "data" / "tidemail" / "mail.db") as cache:
            contents = cache.execute("SELECT data FROM draft_attachments").fetchone()[0]
            assert contents == b"Approved attachment bytes"
        print("PASS automatic compose, multiline Ripple body, attachment browser, draft, resize")

        undo = Session(binary, ["send"], env, work)
        compose_to_review(undo)
        undo.send("j\r")
        undo.expect("Sending in")
        undo.send("u")
        undo.expect("Send undone")
        undo.send("\r")
        undo.expect("drafted")
        undo.finish()
        print("PASS durable queue and undo without SMTP submission")

        pipe = Session(binary, ["send", "--interactive", "--json", "--to", "you@example.com",
                               "--subject", "Piped body"], env, work,
                       piped="Piped contents", json_output=True)
        pipe.expect("Sender account")
        for expected in ("2/9  To", "CC (optional)", "BCC (optional)", "5/9  Subject",
                         "Message body"):
            pipe.send("\r")
            pipe.expect(expected)
        pipe.send("\x13")
        pipe.expect("7/9  Signature")
        pipe.send("\r")
        pipe.expect("8/9  Attachments")
        pipe.send("\r")
        pipe.expect("9/9  Review")
        pipe.expect("Piped contents")
        pipe.send("\r")
        result = json.loads(pipe.finish())
        assert result["ok"] and result["data"]["state"] == "drafted"
        print("PASS explicit interactive pipe and clean JSON stdout")

        immediate = Session(binary, ["send", "--interactive", "--send-now"], env, work)
        compose_to_review(immediate)
        immediate.send("j\r")
        immediate.expect("Delivery status")
        immediate.expect("sent")
        immediate.send("\r")
        immediate.finish()
        fixture.thread.join(timeout=5)
        assert fixture.error is None, fixture.error
        assert b"Hello from the wizard" in fixture.message
        assert b"Fixture signature" in fixture.message
        print("PASS approved synchronous delivery through local fake SMTP")


if __name__ == "__main__":
    main()
