"""Explicit character-device passthrough for apps, never privileged containers."""
import re
import stat
import json
import shutil
import subprocess
import hashlib
from os import major as os_major, minor as os_minor
from pathlib import Path
from .core import Error

RAW_USB = re.compile(r'/dev/bus/usb/([0-9]{3})/([0-9]{3})')


def raw_usb_node(path, root='/dev'):
    address = RAW_USB.fullmatch(str(path))
    if not address or not 1 <= int(address[2]) <= 127 or int(address[1]) < 1:
        return False
    try:
        node = (Path(root) / str(path).removeprefix('/dev/')).stat()
        return stat.S_ISCHR(node.st_mode) and os_major(node.st_rdev) == 189 and os_minor(node.st_rdev) == (int(address[1]) - 1) * 128 + int(address[2]) - 1
    except OSError:
        return False


def usb_peripheral(path, root='/sys/bus/usb/devices'):
    """Identify a raw USB peripheral without granting access to host devices.

    USB bus addresses are reused. Only an enumerated device with a persistent,
    unique serial is eligible; interface metadata must be complete and may not
    contain storage, hubs, or network classes. Serial tty interfaces are offered
    independently and do not require access to the entire USB device.
    """
    address = RAW_USB.fullmatch(str(path))
    if address is None:
        return None
    def read(device, key):
        try:
            return (device / key).read_text().strip()[:200]
        except (OSError, UnicodeError):
            return ''
    candidates = []
    for device in Path(root).glob('*'):
        if read(device, 'busnum') == str(int(address[1])) and read(device, 'devnum') == str(int(address[2])):
            candidates.append(device)
    if len(candidates) != 1:
        return None
    device = candidates[0]
    vendor, product, serial = (read(device, key) for key in ('idVendor', 'idProduct', 'serial'))
    if not re.fullmatch('[0-9a-f]{4}', vendor) or not re.fullmatch('[0-9a-f]{4}', product) or not serial:
        return None
    interfaces = list(device.glob(device.name + ':*'))
    classes = [read(device, 'bDeviceClass'), *(read(interface, 'bInterfaceClass') for interface in interfaces)]
    known_classes = {'00', '01', '02', '03', '05', '06', '07', '08', '09', '0a', '0b', '0d', '0e', '0f', '10', 'dc', 'e0', 'ef', 'fe', 'ff'}
    if not interfaces or any(value not in known_classes for value in classes) or set(classes) & {'02', '08', '09', '0a', 'e0'}:
        return None
    if any(value in ('00', 'ef') for value in classes[1:]):
        return None  # An interface needs an actual class, not a composite marker.
    if any(device.rglob('block')) or any(device.rglob('net')):
        return None  # Vendor-specific interfaces can still back host disks/NICs.
    duplicates = sum(read(other, 'idVendor') == vendor and read(other, 'idProduct') == product and read(other, 'serial') == serial for other in Path(root).glob('*'))
    if duplicates != 1:
        return None
    identity = hashlib.sha256(json.dumps({'port': device.name, 'vendor': vendor, 'product': product, 'serial': serial}, sort_keys=True).encode()).hexdigest()
    return {'identity': identity}


def device_properties(path):
    """Read local udev names, never execute a value obtained from a device."""
    try:
        result=subprocess.run(['udevadm','info','--query=property','--name',str(path)],capture_output=True,text=True,timeout=2,check=True)
        return dict(line.split('=',1) for line in result.stdout[:16384].splitlines() if '=' in line)
    except (OSError,subprocess.SubprocessError): return {}


def device_label(path, kind, properties):
    vendor=properties.get('ID_VENDOR_FROM_DATABASE') or properties.get('ID_VENDOR','')
    model=properties.get('ID_MODEL_FROM_DATABASE') or properties.get('ID_MODEL','')
    serial=properties.get('ID_SERIAL_SHORT','')
    description=' '.join(value.replace('_',' ') for value in (vendor,model) if value)
    return f"{description or ('NPU' if kind=='npu' else 'Grafikbeschleuniger' if kind=='gpu' else 'USB-Gerät')}"+(f" · {serial}" if serial else '')+f" · {path}"


def devices(root='/dev', usb_root='/sys/bus/usb/devices'):
    base=Path(root); result=[]; seen=set()
    for pattern,kind in [('serial/by-id/*','usb'),('ttyUSB*','usb'),('ttyACM*','usb'),('bus/usb/*/*','usb'),('dri/renderD*','gpu'),('accel/accel*','npu'),('kfd','gpu')]:
        for path in sorted(base.glob(pattern)):
            try:
                real=path.resolve(strict=True); metadata=real.stat()
                if not real.is_relative_to(base.resolve()) or not stat.S_ISCHR(metadata.st_mode) or metadata.st_rdev in seen: continue
                usb = None
                if re.fullmatch(r'bus/usb/[0-9]{3}/[0-9]{3}', str(real.relative_to(base.resolve()))):
                    if not raw_usb_node('/dev/' + str(real.relative_to(base.resolve())), base): continue
                    usb = usb_peripheral('/dev/' + str(real.relative_to(base.resolve())), usb_root)
                    if usb is None: continue
                seen.add(metadata.st_rdev)
                props=device_properties(real) if root=='/dev' else {}
                label=device_label(real,kind,props)
                if kind in ('gpu','npu') and root=='/dev':
                    sysdev=Path('/sys/dev/char')/f'{os_major(metadata.st_rdev)}:{os_minor(metadata.st_rdev)}'/'device'
                    try:
                        vendor=(sysdev/'vendor').read_text().strip()
                        brand={'0x8086':'Intel iGPU / GPU' if kind=='gpu' else 'Intel NPU','0x1002':'AMD GPU' if kind=='gpu' else 'AMD NPU','0x1022':'AMD NPU','0x10de':'NVIDIA GPU'}.get(vendor,'NPU' if kind=='npu' else 'GPU')
                        pci=sysdev.resolve().name
                        model=subprocess.run(['lspci','-s',pci],capture_output=True,text=True,timeout=2,check=True).stdout.strip().split(': ',1)[-1]
                        label=f'{brand} · {model or pci} · {real}'
                    except (OSError,subprocess.SubprocessError): pass
                if path.name=='kfd': label='AMD ROCm · Compute-Zugang · /dev/kfd'
                result.append({'id':str(path), 'path':str(real),'label':label[:512],'kind':kind,'group':metadata.st_gid, **(usb or {})})
            except OSError: continue
    if root=='/dev' and shutil.which('nvidia-smi') and shutil.which('nvidia-container-runtime'):
        try:
            info=subprocess.run(['docker','info','--format','{{json .Runtimes}}'],capture_output=True,text=True,timeout=5,check=True)
            if 'nvidia' in json.loads(info.stdout):
                query=subprocess.run(['nvidia-smi','--query-gpu=uuid,name','--format=csv,noheader'],capture_output=True,text=True,timeout=5,check=True)
                for row in query.stdout.splitlines()[:16]:
                    identifier,_,label=row.partition(',')
                    if re.fullmatch(r'GPU-[a-fA-F0-9-]{36}',identifier.strip()): result.append({'id':'nvidia:'+identifier.strip(),'path':None,'label':'NVIDIA GPU · '+label.strip(),'kind':'gpu','group':None,'driver':'nvidia'})
        except (OSError,ValueError,subprocess.SubprocessError): pass
    return result[:64]


def validate(value, inventory=None):
    if value is None: return []
    if not isinstance(value,list) or len(value)>16 or any(not isinstance(item,str) for item in value) or len(set(value))!=len(value): raise Error('Maximal 16 unterschiedliche App-Geräte auswählen.')
    found={item['id']:item for item in devices() if item['kind'] in ('usb','gpu','npu')} if inventory is None else {item['id']:item for item in inventory}
    if any(item not in found for item in value): raise Error('Ausgewähltes USB/GPU-Gerät ist nicht mehr verfügbar.',409)
    return value


def apply(definition, app, value, inventory=None):
    if not value: return definition
    if all(isinstance(item,dict) for item in value):
        inventory=value
        for item in value:
            if set(item)-{'id','path','label','kind','group','driver','identity'} or item.get('kind') not in ('usb','gpu','npu') or not isinstance(item.get('id'),str): raise Error('Ungültige gespeicherte App-Geräte.')
            if item.get('driver')=='nvidia':
                if not re.fullmatch(r'nvidia:GPU-[a-fA-F0-9-]{36}',item['id']): raise Error('Ungültige GPU-ID.')
            elif not isinstance(item.get('path'),str) or not re.fullmatch(r'/dev/(?:ttyUSB[0-9]+|ttyACM[0-9]+|bus/usb/[0-9]{3}/[0-9]{3}|dri/renderD[0-9]+|accel/accel[0-9]+|kfd)',item['path']) or type(item.get('group')) is not int or item['group']<0: raise Error('Ungültige gespeicherte Gerätezuordnung.')
            if 'identity' in item and (not isinstance(item['identity'], str) or not re.fullmatch('[0-9a-f]{64}', item['identity'])): raise Error('Ungültige gespeicherte Geräteidentität.')
        value=[item['id'] for item in value]
    else:
        inventory=devices() if inventory is None else inventory
        validate(value,inventory)
    found={item['id']:item for item in inventory}
    service=definition['services'][app]
    physical=[found[item] for item in value if found[item].get('driver')!='nvidia'];gpus=[item.removeprefix('nvidia:') for item in value if found[item].get('driver')=='nvidia']
    if physical: service['devices']=[item['path']+':'+item['path']+':rw' for item in physical]
    if gpus: service['deploy']={'resources':{'reservations':{'devices':[{'driver':'nvidia','device_ids':gpus,'capabilities':['gpu']}]}}}
    service['group_add']=[str(group) for group in sorted({item['group'] for item in physical})]
    service.setdefault('labels', {})['io.titan.hardware'] = json.dumps([found[item] for item in value], separators=(',', ':'), sort_keys=True)
    return definition


def hardware_ready(configured, inventory=None):
    if not configured:
        return
    if not isinstance(configured, list) or not all(isinstance(item, dict) for item in configured):
        raise Error('Ungültige gespeicherte App-Geräte.', 409)
    apply({'services': {'check': {}}}, 'check', configured)  # Validate saved shape.
    current = {item['id']: item for item in (devices() if inventory is None else inventory)}
    for item in configured:
        fresh = current.get(item['id'])
        if not fresh or any(fresh.get(key) != item.get(key) for key in ('path', 'kind', 'group', 'driver')):
            raise Error('App-Gerät fehlt oder wurde neu zugeordnet. Geräteauswahl erneut prüfen.', 409)
        if RAW_USB.fullmatch(item.get('path') or '') and (not item.get('identity') or fresh.get('identity') != item['identity']):
            raise Error('USB-Gerät wurde ersetzt oder seine Identität ist nicht gesichert. Geräteauswahl erneut prüfen.', 409)


def native_hardware(row):
    """Recover the pinned selection, never adopt a reused raw USB address."""
    host = row.get('HostConfig') or {}
    physical = [item.get('PathOnHost') for item in host.get('Devices') or []]
    nvidia = ['nvidia:' + value for request in host.get('DeviceRequests') or [] if request.get('Driver') == 'nvidia' for value in request.get('DeviceIDs') or []]
    selected = physical + nvidia
    if not selected:
        return []
    label = ((row.get('Config') or {}).get('Labels') or {}).get('io.titan.hardware')
    if label:
        try:
            configured = json.loads(label)
            apply({'services': {'check': {}}}, 'check', configured)
            mapped = [item['path'] if item.get('driver') != 'nvidia' else item['id'] for item in configured]
            if len(mapped) != len(set(mapped)) or set(mapped) != set(selected):
                raise ValueError('Changed mapping')
        except (ValueError, TypeError, KeyError, Error):
            raise Error('Container-Gerätezuordnung wurde verändert. Geräteauswahl erneut prüfen.', 409) from None
    else:
        if any(RAW_USB.fullmatch(value or '') for value in physical):
            raise Error('Die Identität des USB-Geräts ist nicht gesichert. Geräteauswahl erneut prüfen.', 409)
        inventory = devices()
        configured = [next((item for item in inventory if value in (item['id'], item.get('path'))), None) for value in selected]
        if any(item is None for item in configured):
            raise Error('Container-Gerät ist nicht mehr verfügbar.', 409)
    hardware_ready(configured)
    return configured


def raw_usb_container_ready(row, usb_root='/sys/bus/usb/devices', dev_root='/dev'):
    """Offline daemon guard: only Sysfs and device metadata, never a CLI.

    Restart policies can bypass API admission. A raw USB mapping must therefore
    retain the selected identity even before Docker is allowed to restore it.
    """
    host = row.get('HostConfig') or {}
    selected = host.get('Devices') or []
    if not isinstance(selected, list) or len(selected) > 64:
        raise Error('Ungültige Container-Gerätezuordnung.', 409)
    paths = []
    for item in selected:
        if not isinstance(item, dict): raise Error('Ungültige Container-Gerätezuordnung.', 409)
        path = item.get('PathOnHost')
        if isinstance(path, str) and path.startswith('/dev/bus/usb'):
            if not RAW_USB.fullmatch(path): raise Error('Ungültige RohUSB-Zuordnung.', 409)
            paths.append(path)
    if not paths:
        return
    try:
        label = ((row.get('Config') or {}).get('Labels') or {}).get('io.titan.hardware')
        if not isinstance(label, str) or len(label) > 32768: raise ValueError()
        configured = json.loads(label)
        if not isinstance(configured, list) or not 1 <= len(configured) <= 16 or not all(isinstance(item, dict) for item in configured): raise ValueError()
        recorded = {item.get('path'): item.get('identity') for item in configured if item.get('path') in paths}
        if set(recorded) != set(paths) or len(recorded) != len(paths): raise ValueError()
    except (ValueError, TypeError, AttributeError):
        raise Error('Die Identität des USB-Geräts ist nicht gesichert. Geräteauswahl erneut prüfen.', 409) from None
    for path in paths:
        current = usb_peripheral(path, usb_root)
        if not raw_usb_node(path, dev_root) or current is None or current['identity'] != recorded[path]:
            raise Error('USB-Gerät fehlt, wurde ersetzt oder gehört zum NAS. Start ist gesperrt.', 409)


class AppDevicesMixin:
    def app_devices_ready(self, record):
        configured=record.get("hardware") or []
        hardware_ready(configured)

    def op_app_devices(self):
        return {'devices':devices(),'notes':['Nur tatsächlich erkannte Geräte mit aktivem Treiber werden angeboten. USB-Geräte werden mit Hersteller, Modell und Seriennummer angezeigt, soweit der Host diese meldet.', 'Intel-Grafik und NPU können gemeinsam ausgewählt werden. AMD-Compute benötigt je nach App zusätzlich den ROCm-Zugang. NVIDIA benötigt Container Toolkit; jede App muss die gewählte Beschleunigung unterstützen.']}
