"""Ensure signing configuration is checked without leaking private key material."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT=Path(__file__).resolve().parents[1]

@unittest.skipUnless(shutil.which('openssl'), 'openssl required')
class EarlySigningKeyTests(unittest.TestCase):
    def test_key_match_mismatch_and_invalid_input_do_not_leak_secrets_or_leave_private_files(self):
        with tempfile.TemporaryDirectory() as temporary:
            root=Path(temporary); (root/'titan-build').mkdir(); (root/'.titan').mkdir(); (root/'runner').mkdir()
            script=root/'titan-build/check-signing-key.sh'
            shutil.copyfile(ROOT/'check-signing-key.sh',script)
            key=root/'key.pem'; other=root/'other.pem'
            for path in (key,other):
                subprocess.run(['openssl','genpkey','-algorithm','ED25519','-out',str(path)],check=True,capture_output=True)
            subprocess.run(['openssl','pkey','-in',str(key),'-pubout','-out',str(root/'.titan/release-public.pem')],check=True,capture_output=True)
            for value,expected,message in [(key.read_text(),0,'matches'),(other.read_text(),1,'does not match'),
                                           ('SECRET-INVALID-PRIVATE-KEY',1,'not a valid'),('',1,'required')]:
                with self.subTest(message=message):
                    result=subprocess.run(['bash',str(script)],env={**os.environ,'RUNNER_TEMP':str(root/'runner'),'TITAN_SIGNING_KEY':value},capture_output=True,text=True)
                    self.assertEqual(result.returncode,expected,result.stderr)
                    self.assertIn(message,result.stdout+result.stderr)
                    if value:self.assertNotIn(value,result.stdout+result.stderr)
                    self.assertEqual(list((root/'runner').iterdir()),[])


if __name__=='__main__': unittest.main()
