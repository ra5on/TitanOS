"""Optional plugin/configuration checks, exclusively inside the disposable NAS."""
import json
import shlex
import subprocess
import time


def check_tailscale(request, ssh, ssh_options, ssh_port, plugins):
    if not (plugins / 'tailscale.raw').is_file():
        return

    def remote(script):
        return subprocess.check_output([*ssh, 'sudo sh -ec ' + shlex.quote(script)], text=True).strip()

    for action in ['ignore', 'reboot', 'poweroff']:
        state = request('/api/v1/system/power-button', {'action': action}, method='PUT')
        assert state['settings']['action'] == action, state
        actual = remote('busctl get-property org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager HandlePowerKey')
        assert actual == f's "{action}"', actual
    files = [plugins / 'tailscale.raw', plugins / 'tailscale.plugin.json']
    signed = (plugins / 'tailscale.plugin.json.minisig').is_file()
    if signed:
        files.append(plugins / 'tailscale.plugin.json.minisig')
    subprocess.run(['scp', *ssh_options, '-P', str(ssh_port), *map(str, files), 'betatester@127.0.0.1:/mnt/smoketest/plugins/'], check=True, stdout=subprocess.DEVNULL)
    state = request('/api/v1/plugins', {'path': '/mnt/smoketest/plugins/tailscale.plugin.json', 'volume': 'smoketest', 'confirm': '' if signed else 'tailscale'})
    plugin = next(p for p in state['plugins'] if p['name'] == 'tailscale' and p['installed'])
    assert plugin['active'] and plugin['official'] == signed, plugin
    state = request('/api/v1/plugins/tailscale')
    assert state['active'] and state['state'] == 'NeedsLogin', state
    original = state['settings']
    changed = dict(original, routes=['192.0.2.0/24'], advertiseExit=True)
    trial = request('/api/v1/plugins/tailscale', changed, method='PUT')
    assert trial['pendingUntil'], trial
    assert remote('cat /proc/sys/net/ipv4/ip_forward') == '1'
    assert remote('cat /proc/sys/net/ipv6/conf/all/forwarding') == '1'
    request('/api/v1/plugins/tailscale/control/undo', {})
    assert request('/api/v1/plugins/tailscale')['settings'] == original
    request('/api/v1/plugins/tailscale', changed, method='PUT')
    request('/api/v1/plugins/tailscale/control/confirm', {})
    request('/api/v1/plugins/tailscale/disable', {})
    assert remote('test -f /mnt/smoketest/.titanos/tailscale/tailscaled.state && echo retained') == 'retained'
    request('/api/v1/plugins/tailscale/enable', {})
    assert request('/api/v1/plugins/tailscale')['settings'] == changed
    # Synthetic chains test the ordering required for Tailscale's subnet SNAT.
    remote('for fw in iptables ip6tables; do $fw -N ts-input 2>/dev/null || true; $fw -N ts-forward 2>/dev/null || true; done; /usr/libexec/titanos-firewall-open; /usr/libexec/titanos-firewall-open')
    for fw in ['iptables', 'ip6tables']:
        rules = remote(f'{fw} -S FORWARD').splitlines()
        assert rules[1] == '-A FORWARD -j ts-forward' and rules.count('-A FORWARD -j ts-forward') == 1, rules
    request('/api/v1/plugins/tailscale/disable', {})
    request('/api/v1/plugins/tailscale', method='DELETE')
    print('Power button reload, signed Tailscale lifecycle, routing confirmation/undo and firewall hook order passed', flush=True)
