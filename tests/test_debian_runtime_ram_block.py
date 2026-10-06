"""Execute the runtime shell against private programs, never host services.

The shared Python proof has its own unit/report security tests. Here it is a
private fixture so these tests verify that the shell only calls it after real
bounded start attempts, complete program checks, and failed daemon end states.
"""
import json
import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import time
import unittest


ROOT = Path(__file__).resolve().parents[1]


PROGRAM = r'''#!/usr/bin/python3
import json, os, pathlib, sys, time
name = pathlib.Path(sys.argv[0]).name
args = sys.argv[1:]
mode = os.environ['FIXTURE_MODE']
log = pathlib.Path(os.environ['FIXTURE_LOG'])
with log.open('a') as out:
    out.write(json.dumps({'program': name, 'args': args}) + '\n')
if name == 'uname':
    print('aarch64' if mode == 'wrong_architecture' else 'x86_64')
elif name == 'docker':
    if args == ['compose', 'version']:
        if mode == 'missing_compose': sys.exit(1)
        print('Docker Compose version fixture')
    elif args[:1] == ['info']:
        if mode == 'info_defect': sys.exit(1)
        print('fixture-server')
    else: sys.exit(99)
elif name == 'systemctl':
    if args[:1] == ['enable']:
        if mode == 'enable_defect' and args[1:] == ['docker.service']: sys.exit(1)
        if mode == 'socket_defect' and '--now' in args: sys.exit(1)
    elif args[:1] == ['start']:
        if mode == 'start_hangs': time.sleep(10)
        if mode in ('ram_blocked', 'ram_blocked_settles', 'proof_hangs',
                    'never_settles', 'invalid_state', 'unknown_memory',
                    'daemon_defect', 'stale_report', 'socket_defect',
                    'enable_defect', 'one_daemon_ready'):
            if mode == 'one_daemon_ready' and args[-1] == 'docker.service': sys.exit(0)
            sys.exit(1)
    elif args[:2] == ['is-failed', '--quiet']:
        if mode == 'never_settles': sys.exit(1)
        if mode == 'ram_blocked_settles' and args[-1] == 'docker.service':
            counter = log.with_suffix('.counter')
            tries = int(counter.read_text()) if counter.exists() else 0
            counter.write_text(str(tries + 1))
            if tries < 3: sys.exit(1)
        sys.exit(0)
    else: sys.exit(99)
elif name == 'virsh':
    if 'net-autostart' in args and mode == 'network_defect': sys.exit(1)
    if 'net-list' in args: print('default')
elif name not in ('qemu-img', 'qemu-system-x86_64', 'websockify', 'modprobe'):
    sys.exit(99)
'''


PROOF = r'''
import json, os, pathlib, time
def verified_boot_memory_block(units=('docker.service', 'libvirtd.service')):
    log = pathlib.Path(os.environ['FIXTURE_LOG'])
    events = [json.loads(line) for line in log.read_text().splitlines()]
    with log.open('a') as out:
        out.write(json.dumps({'program': 'fresh-proof', 'args': list(units)}) + '\n')
    mode = os.environ['FIXTURE_MODE']
    if mode == 'proof_hangs': time.sleep(10)
    starts = {tuple(event['args']) for event in events if event['program'] == 'systemctl'}
    required = {('start', unit) for unit in units}
    failed = {('is-failed', '--quiet', unit) for unit in units}
    if mode not in ('ram_blocked', 'ram_blocked_settles') or not required <= starts or not failed <= starts:
        return None
    return {'total_bytes': 3 * 1024**3, 'reserve_bytes': 1024**3, 'required_bytes': 4 * 1024**3}
'''


class DebianRuntimeRAMBlockTests(unittest.TestCase):
    def run_runtime(self, mode='healthy', *, missing='', budget=None, component='all'):
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Path(temporary)
            programs = fixture / 'bin'
            programs.mkdir()
            for name in ('docker', 'systemctl', 'virsh', 'qemu-img',
                         'qemu-system-x86_64', 'websockify', 'uname', 'modprobe'):
                program = programs / name
                program.write_text(PROGRAM)
                program.chmod(0o700)
            console = fixture / 'rfb.js'
            if missing != 'noVNC': console.write_text('// fixture\n')
            library = fixture / 'component-functions.sh'
            library.write_text((ROOT / 'scripts/component-functions.sh').read_text().replace(
                '/usr/share/novnc/core/rfb.js', str(console)))
            package = fixture / 'lib/titan'
            package.mkdir(parents=True)
            (package / '__init__.py').write_text('')
            (package / 'debian_updates.py').write_text(PROOF)
            log = fixture / 'commands.jsonl'
            log.touch()
            # A stale report is deliberately present. The runtime must not
            # inspect it or infer an exception without the fresh proof.
            (fixture / 'old-memory-report.json').write_text(json.dumps({
                'status': 'insufficient_memory', 'checked_at': 1}))
            source = (ROOT / 'packaging/debian/runtime.sh').read_text()
            source = source[source.index('source /usr/share/titan/component-functions.sh'):]
            source = source.replace('/usr/share/titan/component-functions.sh', str(library))
            source = source.replace('/usr/lib/titan', str(package.parent))
            if budget is not None:
                source = source.replace('SECONDS + 24', 'SECONDS + ' + str(budget))
            script = ('set -euo pipefail\nexport LC_ALL=C.UTF-8\njson=true\ncomponent=' +
                      shlex.quote(component) + '\ncommand() {\n' +
                      ' if [[ "$1" == -v && "$2" == ' + shlex.quote(missing or '__none__') +
                      ' ]]; then return 1; fi\n builtin command "$@";\n}\n' + source)
            start = time.monotonic()
            result = subprocess.run(['/bin/bash', '-c', script], capture_output=True, text=True,
                timeout=10, env={**os.environ, 'PATH': str(programs) + ':/usr/bin:/bin',
                                 'FIXTURE_MODE': mode, 'FIXTURE_LOG': str(log)})
            elapsed = time.monotonic() - start
            events = [json.loads(line) for line in log.read_text().splitlines()]
            report = json.loads(result.stdout)
            return result, report, events, elapsed

    def starts(self, events):
        return [event['args'] for event in events
                if event['program'] == 'systemctl' and event['args'][:1] == ['start']]

    def test_healthy_daemons_are_verified_without_guard_exception(self):
        result, report, events, _ = self.run_runtime()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(report['ok'])
        self.assertEqual(self.starts(events), [['start', 'docker.service'], ['start', 'libvirtd.service']])
        self.assertTrue(any(event['program'] == 'docker' and event['args'][:1] == ['info'] for event in events))
        self.assertTrue(any(event['program'] == 'virsh' for event in events))
        self.assertFalse(any(event['program'] == 'fresh-proof' for event in events))

    def test_confirmed_memory_block_keeps_management_ready_but_not_daemons(self):
        result, report, events, _ = self.run_runtime('ram_blocked')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(report['ok'])
        self.assertEqual({row['state'] for row in report['components'].values()}, {'ram_blocked'})
        self.assertTrue(all(not row['ok'] for row in report['components'].values()))
        self.assertTrue(any('RAM-Schutz' in warning for warning in report['warnings']))
        self.assertEqual(self.starts(events), [['start', 'docker.service'], ['start', 'libvirtd.service']])
        proof = next(index for index, event in enumerate(events) if event['program'] == 'fresh-proof')
        self.assertTrue(all(index < proof for index, event in enumerate(events)
                            if event['program'] == 'systemctl' and event['args'][:1] == ['start']))
        self.assertFalse(any(event['program'] == 'virsh' for event in events))
        self.assertFalse(any(event['program'] == 'docker' and event['args'][:1] == ['info'] for event in events))

    def test_auto_restart_must_settle_before_fresh_proof(self):
        result, report, events, _ = self.run_runtime('ram_blocked_settles')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(report['ok'])
        probes = [event for event in events if event['program'] == 'systemctl'
                  and event['args'] == ['is-failed', '--quiet', 'docker.service']]
        self.assertGreaterEqual(len(probes), 4)
        self.assertEqual(events[-1]['program'], 'fresh-proof')

    def test_invalid_unknown_stale_and_daemon_errors_fail_closed(self):
        for mode in ('invalid_state', 'unknown_memory', 'daemon_defect', 'stale_report'):
            with self.subTest(mode=mode):
                result, report, events, _ = self.run_runtime(mode)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertFalse(report['ok'])
                self.assertEqual({row['state'] for row in report['components'].values()}, {'failed'})
                self.assertEqual(sum(event['program'] == 'fresh-proof' for event in events), 1)

    def test_every_program_and_compose_are_required_before_exception(self):
        for missing in ('docker', 'virsh', 'qemu-img', 'qemu-system-x86_64', 'websockify', 'noVNC'):
            with self.subTest(missing=missing):
                result, report, events, _ = self.run_runtime('ram_blocked', missing=missing)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertFalse(report['ok'])
                self.assertFalse(any(event['program'] == 'fresh-proof' for event in events))
        result, report, events, _ = self.run_runtime('missing_compose')
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertFalse(report['ok'])
        self.assertFalse(any(event['program'] == 'fresh-proof' for event in events))
        self.assertNotIn(['start', 'docker.service'], self.starts(events))

    def test_wrong_architecture_and_service_setup_defects_are_not_ram_mode(self):
        for mode in ('wrong_architecture', 'socket_defect', 'enable_defect', 'info_defect',
                     'network_defect', 'one_daemon_ready'):
            with self.subTest(mode=mode):
                result, report, events, _ = self.run_runtime(mode)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertFalse(report['ok'])
                self.assertFalse(any(event['program'] == 'fresh-proof' for event in events))
                self.assertNotIn('ram_blocked', {row['state'] for row in report['components'].values()})

    def test_normal_selected_component_never_starts_the_other(self):
        for component, unit in (('docker', 'docker.service'), ('vms', 'libvirtd.service')):
            with self.subTest(component=component):
                result, report, events, _ = self.run_runtime(component=component)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertTrue(report['ok'])
                self.assertEqual(set(report['components']), {component})
                self.assertEqual(self.starts(events), [['start', unit]])

    def test_hung_start_and_unsettled_restart_obey_shared_budget(self):
        for mode in ('start_hangs', 'never_settles'):
            with self.subTest(mode=mode):
                result, report, events, elapsed = self.run_runtime(mode, budget=2)
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertFalse(report['ok'])
                self.assertLess(elapsed, 4.5)
                self.assertFalse(any(event['program'] == 'fresh-proof' for event in events))

    def test_fresh_proof_has_no_independent_unbounded_startup_budget(self):
        result, report, events, elapsed = self.run_runtime('proof_hangs', budget=2)
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertFalse(report['ok'])
        self.assertLess(elapsed, 4.5)
        self.assertEqual(sum(event['program'] == 'fresh-proof' for event in events), 1)


if __name__ == '__main__':
    unittest.main()
