#!/usr/bin/env python3
"""Verify only this checkout's App/CLI against disposable data."""
import json, os, pathlib, signal, subprocess, tempfile, time
ROOT = pathlib.Path(__file__).resolve().parents[1]
APP = pathlib.Path(os.environ.get('APILOT_VERIFY_APP', str(ROOT/'bin/apilot-app')))
subprocess.run(['python3', str(ROOT/'scripts/verify-built-app.py')],check=True)
CLI = ROOT/'bin/apilot'
WORK = ROOT/'tmp/lifecycle'
WORK.mkdir(parents=True, exist_ok=True)

def wait_for(fn, seconds=10):
    end=time.monotonic()+seconds
    while time.monotonic()<end:
        value=fn()
        if value: return value
        time.sleep(.025)
    raise AssertionError('condition did not become true')

def alive(pid):
    try: os.kill(pid,0); return True
    except ProcessLookupError: return False

results=[]
with tempfile.TemporaryDirectory(prefix='apilot-lifecycle-', dir=WORK) as directory:
    env=dict(os.environ, APILOT_DATA_DIR=directory)
    def cli(*args, expected=0):
        p=subprocess.run([str(CLI),'--json',*args],env=env,capture_output=True,text=True,timeout=4)
        assert p.returncode==expected,(args,p.returncode,p.stdout,p.stderr)
        return json.loads(p.stdout)
    def start():
        log=open(WORK/'app.log','w')
        p=subprocess.Popen([str(APP)],env=env,stdout=log,stderr=log)
        wait_for(lambda: 'tray created' in (WORK/'app.log').read_text())
        return p,log
    def finish(p,log,kind):
        p.send_signal(signal.SIGKILL if kind=='crash' else signal.SIGTERM)
        p.wait(timeout=10);log.close()
    try:
        for kind in ('quit','crash'):
            for i in range(10):
                p,log=start()
                name=f'{kind}-{i}'
                pidfile=pathlib.Path(directory)/f'{name}.pids'
                cli('task','add',name,'--', '/bin/sh',str(ROOT/'scripts/fixtures/process_tree.sh'),str(pidfile))
                run=cli('task','run',name)['data']
                wait_for(lambda: cli('run','show',run['id'])['data']['status']=='running')
                wait_for(lambda: pidfile.exists() and len(pidfile.read_text().split())==2)
                pids=list(map(int,pidfile.read_text().split()))
                started=time.monotonic();finish(p,log,kind)
                wait_for(lambda: all(not alive(pid) for pid in pids))
                elapsed=time.monotonic()-started
                assert elapsed<10,elapsed
                offline=cli('status',expected=3)
                assert offline['error']['code']=='APP_NOT_RUNNING'
                p,log=start()
                restored=cli('run','show',run['id'])['data']
                assert restored['status']==('interrupted' if kind=='crash' else 'canceled'),restored
                assert cli('task','show',name)['data']['name']==name
                finish(p,log,'quit')
                results.append({'mode':kind,'iteration':i+1,'cleanup_seconds':round(elapsed,3),'restored_status':restored['status']})
                print(json.dumps(results[-1]),flush=True)
    finally:
        if 'p' in locals() and p.poll() is None:
            p.terminate()
            try: p.wait(timeout=10)
            except subprocess.TimeoutExpired: p.kill();p.wait()
        if 'log' in locals():log.close()
(WORK/'results.json').write_text(json.dumps(results,indent=2)+'\n')
print(f'PASS: {len(results)} lifecycle repetitions')
