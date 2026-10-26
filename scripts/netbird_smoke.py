"""NetBird plugin checks without a NetBird account, inside the disposable NAS."""
import json
import shlex
import subprocess
import urllib.error


def check_netbird(request, ssh, ssh_options, ssh_port, plugins):
    if not (plugins / 'netbird.raw').is_file():
        return

    def remote(script):
        return subprocess.check_output([*ssh, 'sudo sh -ec ' + shlex.quote(script)], text=True).strip()

    def refused(path, data, method=None):
        try:
            request(path, data, method=method)
        except urllib.error.HTTPError as error:
            return error.code, json.loads(error.read())['error']['code']
        raise AssertionError(f'{path} unexpectedly succeeded')

    files = [plugins / 'netbird.raw', plugins / 'netbird.plugin.json']
    signed = (plugins / 'netbird.plugin.json.minisig').is_file()
    if signed:
        files.append(plugins / 'netbird.plugin.json.minisig')
    subprocess.run(['scp', *ssh_options, '-P', str(ssh_port), *map(str, files), 'betatester@127.0.0.1:/mnt/smoketest/plugins/'], check=True, stdout=subprocess.DEVNULL)
    state = request('/api/v1/plugins', {'path': '/mnt/smoketest/plugins/netbird.plugin.json', 'volume': 'smoketest', 'confirm': '' if signed else 'netbird'})
    plugin = next(p for p in state['plugins'] if p['name'] == 'netbird' and p['installed'])
    assert plugin['active'] and plugin['official'] == signed, plugin
    # A clean, unregistered client: the daemon runs, nothing connects.
    assert remote('systemctl is-active titanos-netbird.service') == 'active'
    assert remote('netbird version') == json.loads((plugins / 'netbird.plugin.json').read_text())['contains']['en'][0].split()[-1]
    state = request('/api/v1/plugins/netbird')
    assert state['active'] and state['state'] == 'NeedsLogin' and not state['setupKeySet'] and not state['connect'], state
    assert state['version'] and state['ips'] == [] and 'pendingUntil' not in state, state
    peers = request('/api/v1/plugins/netbird/peers')
    assert peers['active'] and peers['state'] == 'NeedsLogin' and peers['peers'] == [], peers
    assert refused('/api/v1/plugins/netbird/control/connect', {}, 'POST') == (409, 'netbird_setup_key')
    # Keys and state live on the data volume, private to root, never in RAM.
    assert remote('stat -c %a:%U /mnt/smoketest/.titanos/netbird') == '700:root'
    assert remote('tr "\\0" "\\n" < /proc/$(systemctl show -p MainPID --value titanos-netbird.service)/environ | grep ^NB_STATE_DIR=') == 'NB_STATE_DIR=/mnt/smoketest/.titanos/netbird'
    # Its interface is accepted before Tailscale's hooks (synthetic ts-input).
    remote('iptables -N ts-input 2>/dev/null || true; iptables -I INPUT 1 -j ts-input; /usr/libexec/titanos-netbird-firewall; /usr/libexec/titanos-netbird-firewall')
    rules = remote('iptables -S INPUT').splitlines()
    wt0 = '-A INPUT -i wt0 -m comment --comment titanos-netbird -j ACCEPT'
    assert rules[1] == wt0 and rules.count(wt0) == 1, rules
    remote('while iptables -C INPUT -j ts-input 2>/dev/null; do iptables -D INPUT -j ts-input; done; iptables -X ts-input')
    # Routing choices run on trial; forwarding is switched on, never off.
    original = state['settings']
    changed = dict(original, serveRoutes=True)
    trial = request('/api/v1/plugins/netbird', changed, method='PUT')
    assert trial['pendingUntil'], trial
    assert remote('cat /proc/sys/net/ipv4/ip_forward') == '1'
    assert refused('/api/v1/plugins/netbird/disable', {}, 'POST')[0] == 409
    request('/api/v1/plugins/netbird/control/undo', {})
    assert request('/api/v1/plugins/netbird')['settings'] == original
    request('/api/v1/plugins/netbird', changed, method='PUT')
    request('/api/v1/plugins/netbird/control/confirm', {})
    # The setup key is stored, reported only as set, and removable.
    key = 'A1B2C3D4-E5F6-4A7B-8C9D-0E1F2A3B4C5D'
    state = request('/api/v1/plugins/netbird/setup-key', {'setupKey': key}, method='PUT')
    assert state['setupKeySet'] and key not in json.dumps(state), state
    state = request('/api/v1/plugins/netbird/setup-key', {'setupKey': ''}, method='PUT')
    assert not state['setupKeySet'], state
    request('/api/v1/plugins/netbird/disable', {})
    assert remote('systemctl is-active titanos-netbird.service || true') != 'active'
    assert remote('iptables -S INPUT | grep -c titanos-netbird || true') == '0'
    assert remote('test -d /mnt/smoketest/.titanos/netbird && echo retained') == 'retained'
    request('/api/v1/plugins/netbird/enable', {})
    assert request('/api/v1/plugins/netbird')['settings'] == changed
    request('/api/v1/plugins/netbird/disable', {})
    request('/api/v1/plugins/netbird', method='DELETE')
    assert remote('test ! -e /usr/bin/netbird && test ! -e /run/extensions/netbird.raw && echo removed') == 'removed'
    print('Signed NetBird lifecycle, unregistered state, setup key, routing trial and firewall hook order passed', flush=True)
