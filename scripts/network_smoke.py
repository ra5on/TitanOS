"""Network checks for the disposable VM created by smoke-release.py."""
import json
import shlex
import subprocess
import time
from pathlib import Path


def check_network(boot, request, directory, ssh_port):
    request("/api/v1/system/ssh", {"enabled": True}, method="PUT")
    ssh = ["ssh", "-i", str(directory / "ssh-key"), "-p", str(ssh_port),
           "-o", "IdentitiesOnly=yes", "-o", "BatchMode=yes", "-o", "ConnectTimeout=3",
           "-o", f"UserKnownHostsFile={directory}/known_hosts", "betatester@127.0.0.1"]

    def remote(script, timeout=60):
        return subprocess.check_output([*ssh, "sudo sh -ec " + shlex.quote(script)],
                                       text=True, stderr=subprocess.STDOUT, timeout=timeout).strip()

    def wait_for(check, timeout=60):
        deadline = time.monotonic() + timeout
        while True:
            try:
                status = request("/api/v1/network")["network"]
                if check(status):
                    return status
            except (OSError, ValueError):
                pass
            if time.monotonic() > deadline:
                raise AssertionError("network state did not converge")
            time.sleep(2)

    def change(cfg):
        try:
            request("/api/v1/network", cfg, method="PUT")
        except OSError:
            # Moving the interface can interrupt the initiating connection.
            # The following check must still observe the pending configuration.
            pass
        return wait_for(lambda n: n["pending"] == cfg and n["device"] == ("br0" if cfg["bridge"] else cfg["ports"][0])
                        and bool(n["addresses"]))

    try:
        deadline = time.monotonic() + 30
        while True:
            try:
                remote("true")
                break
            except subprocess.CalledProcessError:
                if time.monotonic() > deadline:
                    raise
                time.sleep(1)
        state = request("/api/v1/network")["network"]
        port = state["interfaces"][0]["name"]
        mac = state["interfaces"][0]["mac"]
        cfg = dict(state["config"], ports=[port], bondMode="", bridge=True, dhcp=True,
                   address="", gateway="", dns=[], vlan=0)
        if boot == 1:
            change(cfg)
            # Do not confirm the first bridge: the two-minute safety timer must
            # remove it and return the address to the physical NIC unaided.
            wait_for(lambda n: n["pending"] is None and not n["config"]["bridge"]
                     and n["device"] == port and bool(n["addresses"]), 170)
            remote("test ! -d /sys/class/net/br0")
            print("Network: unconfirmed bridge rolled back automatically", flush=True)
            state = change(cfg)
            request("/api/v1/network/confirm", {})
            # Move between DHCP and static addressing on the same live bridge.
            # QEMU's host forwarding continues to point at this same address.
            fixed = dict(cfg, dhcp=False, address=state["addresses"][0], gateway=state["gateway"], dns=["10.0.2.3"])
            change(fixed)
            request("/api/v1/network/confirm", {})
            change(cfg)
            request("/api/v1/network/confirm", {})
        else:
            assert state["config"]["bridge"] and state["device"] == "br0", state
            print("Network: confirmed bridge restored after reboot", flush=True)

        assert remote("cat /sys/class/net/br0/address") == mac
        addresses = json.loads(remote(f"ip -j -4 address show dev {shlex.quote(port)}"))
        assert not addresses[0]["addr_info"], addresses
        remote("systemctl is-active --quiet titanos-firewall-open.service; test ! -e /etc/libvirt/qemu/networks/autostart/default.xml")

        # A separate guest namespace is attached just like a libvirt tap. Its
        # MAC must reach QEMU's DHCP server through the actual networkd bridge.
        remote("""ip netns add titan-smoke
ip link add ts-vm type veth peer name eth0 netns titan-smoke
ip link set ts-vm master br0
ip link set ts-vm up
ip -n titan-smoke link set lo up
ip -n titan-smoke link set eth0 address 52:54:00:ba:ad:01
ip -n titan-smoke link set eth0 up
cat > /run/titan-smoke-dhcp <<'EOF'
#!/bin/sh
[ "$1" = bound ] || exit 0
ip address replace "$ip/24" dev "$interface"
ip route replace default via "$router" dev "$interface"
printf '%s' "$ip" > /run/titan-smoke-ip
EOF
chmod 755 /run/titan-smoke-dhcp
ip netns exec titan-smoke busybox udhcpc -i eth0 -n -q -t 5 -T 2 -s /run/titan-smoke-dhcp
test -s /run/titan-smoke-ip
ip netns exec titan-smoke ping -c 1 -W 3 10.0.2.2""")
        guest_ip = remote("cat /run/titan-smoke-ip")
        # Ordinary reconfiguration must preserve the live guest's bridge port.
        change(cfg)
        request("/api/v1/network/confirm", {})
        remote("test -e /sys/class/net/br0/brif/ts-vm; ip netns exec titan-smoke ping -c 1 -W 3 10.0.2.2")
        assert remote("cat /run/titan-smoke-ip") == guest_ip

        # Use only the disposable volume; no Docker images or external registry
        # are needed to exercise real daemon/network firewall initialization.
        remote("""mkdir -p /etc/docker /mnt/smoketest/.titanos/network-smoke
printf '%s\n' '{"data-root":"/mnt/smoketest/.titanos/network-smoke"}' > /etc/docker/daemon.json
systemctl start docker.service
docker network create titan-smoke-net
ip netns exec titan-smoke ping -c 1 -W 3 10.0.2.2
systemctl restart docker.service
docker network rm titan-smoke-net
for fw in iptables ip6tables; do
    for chain in INPUT FORWARD OUTPUT; do
        "$fw" -S "$chain" | head -1 | grep -Fx -- "-P $chain ACCEPT"
        "$fw" -C "$chain" -m comment --comment titanos-firewall-open -j ACCEPT
    done
    "$fw" -C DOCKER-USER -m comment --comment titanos-firewall-open -j ACCEPT
done
iptables -t nat -S POSTROUTING | grep MASQUERADE
ip netns exec titan-smoke ping -c 1 -W 3 10.0.2.2
systemctl stop docker.socket docker.service
ip link delete ts-vm
ip netns delete titan-smoke""", timeout=120)
        print(f"Network boot {boot}: stable MAC, address ownership, guest DHCP, live port and Docker restart passed", flush=True)
    except Exception:
        try:
            Path(f"out/network-smoke-{boot}.log").write_text(remote(
                "ip -brief address; bridge link; iptables-save; ip6tables-save; journalctl -b -u systemd-networkd -u titand -u titanos-firewall-open -u docker --no-pager -n 150"))
        except Exception:
            pass
        raise
    finally:
        request("/api/v1/system/ssh", {"enabled": False}, method="PUT")
