"""Bounded commands inside a verified container; never a shell on the NAS."""
import os
import selectors
import shutil
import subprocess
import time

from .core import Error

MAX_OUTPUT = 65536
GUEST_TIMEOUT = 30
WRAPPER = ('command -v timeout >/dev/null 2>&1 || exit 125; '
           'exec timeout -s KILL 30 sh -c "$1"')


def execute(container, command):
    if type(command) is not str or not command.strip() or len(command.encode()) > 4096 or '\0' in command:
        raise Error('Einen Container-Befehl mit höchstens 4096 Bytes eingeben.')
    executable = shutil.which('docker', path='/usr/sbin:/usr/bin:/sbin:/bin')
    if not executable:
        raise Error('Docker ist nicht installiert.', 503)
    args = [executable, 'exec', container, '/bin/sh', '-c', WRAPPER, 'titan-console', command]
    output = bytearray()
    truncated = False
    timed_out = False
    with subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                          stderr=subprocess.STDOUT, env={'PATH':'/usr/sbin:/usr/bin:/sbin:/bin',
                          'LC_ALL':'C.UTF-8'}) as process, selectors.DefaultSelector() as selector:
        selector.register(process.stdout, selectors.EVENT_READ)
        deadline = time.monotonic() + GUEST_TIMEOUT + 5
        while selector.get_map():
            if time.monotonic() >= deadline:
                timed_out = True
                process.kill()
                break
            for key, _ in selector.select(min(1, max(0, deadline - time.monotonic()))):
                chunk = os.read(key.fd, 8192)
                if not chunk:
                    selector.unregister(key.fileobj)
                    continue
                remaining = MAX_OUTPUT - len(output)
                output.extend(chunk[:remaining])
                truncated |= len(chunk) > remaining
        code = process.wait(timeout=5)
    if code == 125:
        raise Error('Dieses Image enthält kein timeout-Werkzeug. Die begrenzte Konsole ist hier nicht verfügbar.', 409)
    return {'output': output.decode('utf-8', errors='replace'), 'exit_code': code,
            'truncated': truncated, 'timed_out': timed_out or code in (124, 137)}
