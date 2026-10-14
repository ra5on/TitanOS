#!/usr/bin/env python3
"""Real IPv4/IPv6 TCP traffic, entirely inside disposable network namespaces.

Run: sudo unshare --net python3 scripts/test-bridge-forwarding.py
Or rootless: unshare -Urnm sh -ec 'mount -t tmpfs tmpfs /run/lock;
    python3 scripts/test-bridge-forwarding.py'
Requires iproute2, iptables, br_netfilter and Python's standard library.
"""
import os
from pathlib import Path
import subprocess
import sys
import time

HELPER = Path(__file__).resolve().parents[1] / "build/mkosi.extra/usr/libexec/titanos-bridge-forwarding"
children = []


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout.strip()


def inside(pid, *args):
    return run("nsenter", "-t", str(pid), "-n", *args)


def guest():
    p = subprocess.Popen(["unshare", "--net", "sleep", "300"])
    children.append(p)
    for _ in range(100):
        if p.poll() is not None:
            raise RuntimeError("could not create child network namespace")
        if os.readlink(f"/proc/{p.pid}/ns/net") != os.readlink("/proc/self/ns/net"):
            inside(p.pid, "ip", "link", "set", "lo", "up")
            return p.pid
        time.sleep(0.01)
    raise RuntimeError("namespace creation timed out")


def request(pid, address, expected):
    url = f"http://[{address}]:8123/" if ":" in address else f"http://{address}:8123/"
    code = """import sys, urllib.request
try:
    response = urllib.request.build_opener(urllib.request.ProxyHandler({})).open(sys.argv[1], timeout=0.6)
    assert response.read() == b'bridge-ok'
except (OSError, AssertionError):
    sys.exit(1)
"""
    result = subprocess.run(["nsenter", "-t", str(pid), "-n", sys.executable, "-c", code, url], capture_output=True)
    assert (result.returncode == 0) == expected, f"{url}: wanted reachable={expected}, exit={result.returncode}"


def main():
    # Fail closed if someone accidentally invokes this on the host network.
    try:
        assert os.readlink("/proc/self/ns/net") != os.readlink("/proc/1/ns/net"), "Run inside unshare --net"
    except PermissionError:
        # Rootless user namespaces cannot inspect PID 1; they cannot modify the
        # host netns either. Require the single unprivileged UID mapping.
        mapping = Path("/proc/self/uid_map").read_text().split()
        assert len(mapping) == 3 and mapping[0] == "0" and mapping[1] != "0" and mapping[2] == "1"
    run("ip", "link", "add", "br0", "type", "bridge")
    run("ip", "link", "set", "br0", "up")
    # Loading physdev also loads bridge netfilter on supported kernels.
    run("iptables", "-A", "FORWARD", "-m", "physdev", "--physdev-is-bridged", "-j", "ACCEPT")
    run("iptables", "-F", "FORWARD")
    for name in ["bridge-nf-call-iptables", "bridge-nf-call-ip6tables"]:
        Path("/proc/sys/net/bridge", name).write_text("1\n")
    Path("/proc/sys/net/ipv4/ip_forward").write_text("1\n")
    Path("/proc/sys/net/ipv6/conf/all/forwarding").write_text("1\n")
    for address in ["192.0.2.1/24", "198.51.100.1/24", "fd42::1/64", "fd42:1::1/64"]:
        run("ip", "addr", "add", address, "dev", "br0", *(["nodad"] if ":" in address else []))
    client, server = guest(), guest()
    for pid, name, suffix in [(client, "lan", "10"), (server, "vm", "20")]:
        run("ip", "link", "add", name, "type", "veth", "peer", "name", "eth0", "netns", str(pid))
        run("ip", "link", "set", name, "master", "br0")
        run("ip", "link", "set", name, "up")
        inside(pid, "ip", "link", "set", "eth0", "up")
        inside(pid, "ip", "addr", "add", f"192.0.2.{suffix}/24", "dev", "eth0")
        inside(pid, "ip", "addr", "add", f"fd42::{suffix}/64", "dev", "eth0", "nodad")
    inside(server, "ip", "addr", "add", "198.51.100.20/24", "dev", "eth0")
    inside(server, "ip", "addr", "add", "fd42:1::20/64", "dev", "eth0", "nodad")
    inside(client, "ip", "route", "add", "198.51.100.0/24", "via", "192.0.2.1")
    inside(client, "ip", "-6", "route", "add", "fd42:1::/64", "via", "fd42::1")
    # Keep the routed negative controls routed even after the baseline succeeds.
    inside(client, "sysctl", "-qw", "net.ipv4.conf.eth0.accept_redirects=0", "net.ipv4.conf.all.accept_redirects=0", "net.ipv6.conf.eth0.accept_redirects=0")
    server_code = """import http.server, socket, sys
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200); self.end_headers(); self.wfile.write(b'bridge-ok')
    def log_message(self, *args): pass
class Server(http.server.HTTPServer):
    address_family = socket.AF_INET6 if sys.argv[1] == '6' else socket.AF_INET
Server(('::' if sys.argv[1] == '6' else '0.0.0.0', 8123), Handler).serve_forever()
"""
    # IPv6-only socket avoids sharing IPv4's listener.
    inside(server, "sysctl", "-qw", "net.ipv6.bindv6only=1")
    for family in ["4", "6"]:
        children.append(subprocess.Popen(["nsenter", "-t", str(server), "-n", sys.executable, "-c", server_code, family]))
    # IPv6 link-local addresses undergo duplicate-address detection even though
    # the fixture's global addresses use nodad; routing needs them ready too.
    time.sleep(2)
    bridged = ["192.0.2.20", "fd42::20"]
    routed = ["198.51.100.20", "fd42:1::20"]
    for address in bridged + routed:
        request(client, address, True)
    print("Baseline: IPv4/IPv6 TCP port 8123 reachable", flush=True)
    for firewall in ["iptables", "ip6tables"]:
        run(firewall, "-P", "FORWARD", "DROP")
        run(firewall, "-N", "DOCKER-USER")
        run(firewall, "-A", "DOCKER-USER", "-j", "RETURN")
        run(firewall, "-A", "FORWARD", "-j", "DOCKER-USER")
    for address in bridged:
        request(client, address, False)
    print("Reproduced: Docker-style FORWARD DROP blocks bridged guests", flush=True)
    for _ in range(3):
        run(str(HELPER))
    for address in bridged:
        request(client, address, True)
    for address in routed:
        request(client, address, False)
    for firewall in ["iptables", "ip6tables"]:
        assert run(firewall, "-S", "FORWARD").startswith("-P FORWARD DROP")
        assert run(firewall, "-S", "DOCKER-USER").count("titanos-vm-bridge") == 1
        # A missing hook after firewall reinitialization is restored as well.
        run(firewall, "-D", "FORWARD", "-j", "DOCKER-USER")
    run(str(HELPER))
    for address in bridged:
        request(client, address, True)
    print("Fixed: IPv4/IPv6 work; routed traffic stays blocked; rules are idempotent", flush=True)
    run("ip", "link", "add", "docker-test", "type", "bridge")
    run("ip", "link", "set", "docker-test", "up")
    for port in ["lan", "vm"]:
        run("ip", "link", "set", port, "master", "docker-test")
    for address in bridged:
        request(client, address, False)
    print("Isolation preserved: unrelated bridges remain blocked", flush=True)


try:
    main()
finally:
    for child in reversed(children):
        child.terminate()
        child.wait()
