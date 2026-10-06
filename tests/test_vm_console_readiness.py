"""Real loopback probes must prove RFB readiness without joining a session."""
from contextlib import contextmanager
import socket
import threading
import time
import unittest

from titan.core import Error
from titan.host import Host


@contextmanager
def greeting_server(chunks, *, pause=0, hold=False):
    listener = socket.socket()
    listener.bind(("127.0.0.1", 0))
    listener.listen()
    listener.settimeout(.05)
    stop = threading.Event()
    client_bytes = []
    errors = []

    def serve():
        while not stop.is_set():
            try:
                connection, _ = listener.accept()
            except socket.timeout:
                continue
            except OSError:
                return
            with connection:
                connection.settimeout(.5)
                try:
                    for chunk in chunks:
                        if pause:
                            stop.wait(pause)
                        connection.sendall(chunk)
                    if hold:
                        stop.wait(.5)
                    else:
                        connection.shutdown(socket.SHUT_WR)
                    client_bytes.append(connection.recv(1))
                except (BrokenPipeError, ConnectionResetError):
                    client_bytes.append(b"")
                except OSError as exc:
                    errors.append(exc)

    worker = threading.Thread(target=serve, daemon=True)
    worker.start()
    try:
        yield listener.getsockname()[1], client_bytes
    finally:
        stop.set()
        listener.close()
        worker.join(1)
        if worker.is_alive():
            raise AssertionError("Greeting server did not stop")
        if errors:
            raise AssertionError(errors)


class ConsoleReadinessTests(unittest.TestCase):
    def test_real_fragmented_banner_is_read_without_sending_client_handshake(self):
        with greeting_server([b"RF", b"B 00", b"3.", b"008\n"], pause=.015) as (port, sent):
            Host._wait_console_vnc(port, time.monotonic() + 1)
        self.assertEqual(sent, [b""])

    def test_server_greeting_versions_remain_compatible(self):
        for version in (b"003.003", b"003.007", b"003.008"):
            with self.subTest(version=version), greeting_server([b"RFB " + version + b"\n"]) as (port, sent):
                Host._wait_console_vnc(port, time.monotonic() + 1)
            self.assertEqual(sent, [b""])

    def test_open_tcp_port_with_another_service_is_not_reported_ready(self):
        with greeting_server([b"HTTP/1.1 200 OK\r\n"]) as (port, sent):
            with self.assertRaisesRegex(Error, "nicht als VNC-Server") as failure:
                Host._wait_console_vnc(port, time.monotonic() + 1)
        self.assertEqual(failure.exception.status, 503)
        self.assertEqual(sent, [b""])

    def test_incomplete_greetings_retry_but_do_not_extend_overall_deadline(self):
        with greeting_server([b"RFB 003."]) as (port, sent):
            started = time.monotonic()
            with self.assertRaisesRegex(Error, "noch nicht bereit") as failure:
                Host._wait_console_vnc(port, started + .15)
            elapsed = time.monotonic() - started
        self.assertEqual(failure.exception.status, 503)
        self.assertGreaterEqual(len(sent), 2)
        self.assertTrue(all(data == b"" for data in sent))
        self.assertLess(elapsed, .8)

    def test_silent_server_cannot_block_past_readiness_deadline(self):
        with greeting_server([], hold=True) as (port, sent):
            started = time.monotonic()
            with self.assertRaisesRegex(Error, "noch nicht bereit"):
                Host._wait_console_vnc(port, started + .15)
            elapsed = time.monotonic() - started
        self.assertEqual(sent, [b""])
        self.assertLess(elapsed, .8)

    def test_expired_deadline_does_not_connect(self):
        with self.assertRaisesRegex(Error, "noch nicht bereit"):
            Host._wait_console_vnc(5900, time.monotonic() - 1)


if __name__ == "__main__":
    unittest.main()
