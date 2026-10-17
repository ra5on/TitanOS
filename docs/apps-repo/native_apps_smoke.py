"""Functional checks against fresh disposable installations of the native apps.

Tests create private test accounts and a tiny model/image. Never target user data.
The standalone runner below is restricted to an ephemeral GitHub Actions runner.
"""
import base64
import hashlib
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request
import uuid


def api(url, path, data=None, token=None, raw=False, content_type='application/json', timeout=600):
    headers={'Content-Type':content_type}
    if token: headers['Authorization']='Bearer '+token
    body=data if isinstance(data,bytes) else json.dumps(data).encode() if data is not None else None
    req=urllib.request.Request(url+path,data=body,headers=headers)
    with urllib.request.urlopen(req,timeout=timeout) as response:
        body=response.read()
        if raw:return body
        if not body:return None
        # Ollama endpoints may return one JSON document or newline-delimited JSON.
        try:return json.loads(body)
        except json.JSONDecodeError:return [json.loads(line) for line in body.splitlines() if line.strip()]


def wait_ready(url,path,timeout=900):
    end=time.monotonic()+timeout
    while time.monotonic()<end:
        try:
            if api(url,path,timeout=10) is not None:return
        except (OSError,ValueError):pass
        time.sleep(5)
    raise TimeoutError('App did not become ready: '+path)


def check_ollama(url,restart):
    wait_ready(url,'/health')
    credentials={'name':'TitanOS smoke test','email':'titanos-smoke@example.com','password':secrets.token_urlsafe(24)}
    user=api(url,'/api/v1/auths/signup',credentials)
    assert user['role']=='admin',user.get('role')
    token=user['token']
    tags=api(url,'/ollama/api/tags',token=token)
    assert not tags['models'],'A fresh installation must not download a model automatically'
    pulled=api(url,'/ollama/api/pull',{'model':'smollm2:135m'},token,timeout=1800)
    last=pulled[-1] if isinstance(pulled,list) else pulled
    assert last.get('status')=='success',last
    result=api(url,'/ollama/api/generate',{'model':'smollm2:135m','prompt':'Say hello in one word.','stream':False,'options':{'num_predict':8,'num_ctx':512}},token)
    if isinstance(result,list):result=result[-1]
    assert result.get('done') and result.get('response','').strip(),'No model inference response'
    print('Ollama + Open WebUI: admin login, private backend, model download and CPU inference passed',flush=True)
    restart()
    wait_ready(url,'/health')
    login=api(url,'/api/v1/auths/signin',{'email':credentials['email'],'password':credentials['password']})
    tags=api(url,'/ollama/api/tags',token=login['token'])
    assert any(m['name']=='smollm2:135m' for m in tags['models'])
    print('Ollama + Open WebUI: account and model persistence after container recreation passed',flush=True)


def check_immich(url,restart):
    wait_ready(url,'/api/server/ping')
    credentials={'name':'TitanOS smoke test','email':'titanos-smoke@example.com','password':secrets.token_urlsafe(24)}
    api(url,'/api/auth/admin-sign-up',credentials)
    login=api(url,'/api/auth/login',{'email':credentials['email'],'password':credentials['password']})
    token=login['accessToken']
    # An original 8x8 RGB PNG generated from a deterministic pixel pattern.
    import struct,zlib
    def chunk(kind,data):return struct.pack('>I',len(data))+kind+data+struct.pack('>I',zlib.crc32(kind+data)&0xffffffff)
    pixels=b''.join(b'\0'+bytes([x*30,y*30,100])*8 for y,x in enumerate(range(8)))
    picture=b'\x89PNG\r\n\x1a\n'+chunk(b'IHDR',struct.pack('>IIBBBBB',8,8,8,2,0,0,0))+chunk(b'IDAT',zlib.compress(pixels))+chunk(b'IEND',b'')
    boundary='titanos-'+uuid.uuid4().hex
    parts=[]
    for key,value in {'fileCreatedAt':'2026-10-16T00:00:00.000Z','fileModifiedAt':'2026-10-16T00:00:00.000Z'}.items():
        parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="{key}"\r\n\r\n{value}\r\n'.encode())
    parts.append(f'--{boundary}\r\nContent-Disposition: form-data; name="assetData"; filename="titanos-test.png"\r\nContent-Type: image/png\r\n\r\n'.encode()+picture+b'\r\n')
    parts.append(f'--{boundary}--\r\n'.encode())
    asset=api(url,'/api/assets',b''.join(parts),token,content_type='multipart/form-data; boundary='+boundary)
    asset_path='/api/assets/'+asset['id']+'/original'
    assert hashlib.sha256(api(url,asset_path,token=token,raw=True)).digest()==hashlib.sha256(picture).digest()
    print('Immich: database initialization, admin login and original image upload/download passed',flush=True)
    restart()
    wait_ready(url,'/api/server/ping')
    login=api(url,'/api/auth/login',{'email':credentials['email'],'password':credentials['password']})
    assert api(url,asset_path,token=login['accessToken'],raw=True)==picture
    print('Immich: database, account and image persistence after container recreation passed',flush=True)


def run():
    if os.environ.get('GITHUB_ACTIONS')!='true':raise SystemExit('Run only on an ephemeral GitHub Actions runner; use functions with an isolated NAS test harness locally.')
    here=Path(__file__).resolve().parent
    with tempfile.TemporaryDirectory(prefix='titanos-apps-') as temp:
        for app,container_port,test in [('ollama',8080,check_ollama),('immich',2283,check_immich)]:
            directory=Path(temp)/app;directory.mkdir(mode=0o700)
            rendered=json.loads(subprocess.check_output(['node',str(here/'native-apps-render.mjs'),app],text=True))
            (directory/'compose.yaml').write_text(rendered['compose'])
            secret=directory/'.env';secret.write_text(''.join(k+'='+v+'\n' for k,v in rendered['env'].items()));secret.chmod(0o600)
            # Test services listen only on loopback, at an ephemeral port.
            with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
            service='open-webui' if app=='ollama' else 'immich-server'
            override=directory/'test.yaml';override.write_text(f'services:\n  {service}:\n    ports: !override\n      - "127.0.0.1:{port}:{container_port}"\n')
            command=['docker','compose','-p','titanos-smoke-'+app,'--project-directory',str(directory),'-f',str(directory/'compose.yaml'),'-f',str(override)]
            def compose(*args):subprocess.run([*command,*args],check=True,timeout=1800)
            def recreate():compose('up','-d','--force-recreate','--wait','--wait-timeout','600')
            try:
                compose('up','-d','--wait','--wait-timeout','600')
                test(f'http://127.0.0.1:{port}',recreate)
            finally:
                # Keep errors observable, without printing the .env or resolved config.
                subprocess.run([*command,'ps'],check=False)
                compose('down','--volumes','--remove-orphans')
                subprocess.run(['sudo','rm','-rf','--',str(directory)],check=True)


if __name__=='__main__':run()
