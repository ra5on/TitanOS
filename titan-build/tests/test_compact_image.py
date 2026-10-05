"""Real QEMU overlays provide target capacity without padding the download."""
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

SPEC = importlib.util.spec_from_file_location('titan_compact_verifier', Path(__file__).resolve().parents[1]/'verify-image.py')
VERIFIER = importlib.util.module_from_spec(SPEC); SPEC.loader.exec_module(VERIFIER)


@unittest.skipUnless(shutil.which('qemu-img') and shutil.which('sgdisk'), 'qemu-utils and gdisk required')
class CompactTargetDiskTests(unittest.TestCase):
    def test_real_gpt_template_and_contents_survive_32_and_64_gib_overlay_targets(self):
        with tempfile.TemporaryDirectory() as directory:
            image = Path(directory)/'compact.img'
            with image.open('wb') as stream: stream.truncate(16 * 1024**2)
            subprocess.run(['sgdisk','--clear','--new=1:2048:+1M','--typecode=1:ef00',str(image)],check=True,capture_output=True)
            with image.open('r+b') as stream:
                stream.seek(1024**2 + 128); stream.write(b'Preserve boot and filesystem content')
            original = hashlib.sha256(image.read_bytes()).digest()
            with image.open('rb') as stream:
                header = VERIFIER.read_header(stream, 1)
                self.assertEqual(header['backup'], image.stat().st_size // 512 - 1)
            for gib in (32, 64):
                overlay = Path(directory)/f'target-{gib}.qcow2'
                subprocess.run(['qemu-img','create','-f','qcow2','-F','raw','-b',str(image),str(overlay),str(gib * 1024**3)],check=True,capture_output=True)
                info = json.loads(subprocess.check_output(['qemu-img','info','--output=json',str(overlay)]))
                self.assertEqual(info['virtual-size'], gib * 1024**3)
                self.assertEqual(info['backing-filename'], str(image))
                self.assertEqual(image.stat().st_size, 16 * 1024**2)
                self.assertEqual(hashlib.sha256(image.read_bytes()).digest(), original)
                self.assertLess(overlay.stat().st_size, 1024**2)
