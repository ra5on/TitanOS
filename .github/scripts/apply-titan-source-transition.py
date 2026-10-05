#!/usr/bin/env python3
"""Apply one locally reviewed source snapshot to the already complete TitanOS repository."""
import gzip,hashlib,os,subprocess
from pathlib import Path
BASE_COMMIT='403234c60309c1b8adbf5e6387451663a353ba90'
EXPECTED_TREE='8ae3417dad1d16e2cf71b450a6f4a09f7b5644d4'
PATCH_SHA256='3fa0030b460e75328345028c456ea95064b3228d5b58f94a3ea19e9636153685'
CONTROL_FILES={'.github/workflows/titan-source-transition.yml','.github/scripts/apply-titan-source-transition.py','.titan/namespace-transition.patch.gz'}
def git(*args,**kwargs):return subprocess.check_output(['git',*args],**kwargs).decode().strip()
if os.environ.get('GITHUB_REPOSITORY')!='ra5on/TitanOS':raise SystemExit('Unexpected repository')
head=git('rev-parse','HEAD')
if head!=os.environ.get('GITHUB_SHA'):raise SystemExit('The source transition must use its exact triggering commit')
if git('rev-parse','HEAD^')!=BASE_COMMIT:raise SystemExit('Source base changed; review before applying')
changed=set(git('diff','--name-only',BASE_COMMIT,head).splitlines())
if changed!=CONTROL_FILES:raise SystemExit('Unexpected changes in the transition control commit')
compressed=Path('.titan/namespace-transition.patch.gz').read_bytes()
if hashlib.sha256(compressed).hexdigest()!=PATCH_SHA256:raise SystemExit('Source patch integrity check failed')
patch=gzip.decompress(compressed)
if len(patch)>50*1024*1024:raise SystemExit('Source patch exceeds the reviewed size budget')
subprocess.run(['git','apply','--index','--binary','--check','-'],input=patch,check=True)
subprocess.run(['git','apply','--index','--binary','-'],input=patch,check=True)
for name in sorted(CONTROL_FILES):subprocess.run(['git','rm','--',name],check=True)
if git('write-tree')!=EXPECTED_TREE:raise SystemExit('The applied source does not match the reviewed tree')
subprocess.run(['python3','titan-build/verify-source.py'],check=True)
subprocess.run(['git','-c','user.name=Codex','-c','user.email=codex@users.noreply.github.com','commit','-m','Adopt the verified fresh TitanOS 2.0.1 runtime, UI and Stable release source'],check=True)
# A normal fast-forward push refuses any concurrent user changes.
subprocess.run(['git','push','origin','HEAD:main'],check=True)
print('Published reviewed TitanOS source tree:',EXPECTED_TREE)
