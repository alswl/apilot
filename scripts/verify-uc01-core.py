#!/usr/bin/env python3
"""Run the UC01 Core/CLI checks against the local protocol fixture and a disposable database."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.request


def wait_for(get, ready, description):
    deadline = time.monotonic() + 10
    while time.monotonic() < deadline:
        result = get()
        if ready(result):
            return result
        time.sleep(0.05)
    raise RuntimeError(f'Timed out waiting for {description}')


def cli_call(cli, env, *args, body=None, check=True):
    command = [str(cli), '--json', 'workbench', *args]
    result = subprocess.run(
        command, input=None if body is None else json.dumps(body),
        text=True, capture_output=True, env=env, timeout=40,
    )
    if not check and result.returncode:
        return None
    if result.returncode:
        raise RuntimeError(f'{" ".join(command[1:])} failed: {result.stdout or result.stderr}')
    return json.loads(result.stdout)['data']


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def stop(process):
    process.terminate()
    try:
        process.wait(timeout=10)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()


def write_config(path, fixture):
    temporary = path.with_suffix('.next')
    temporary.write_text(json.dumps(fixture))
    temporary.replace(path)


def scenarios(cli, env, config, verification, tls):
    def call(*args, **kwargs):
        return cli_call(cli, env, *args, **kwargs)

    def browser_ready(auth):
        return wait_for(
            lambda: call('connector', 'auth-status', auth),
            lambda result: result.get('verification_url'), 'browser notification',
        )

    def scopes(connection_id, trigger):
        def get():
            result = call('connector', 'connection', 'show', connection_id)
            return {scope['scope_ref']: scope for scope in result['scopes']}

        return wait_for(
            get,
            lambda result: len(result) == 2 and all(
                scope.get('last_run', {}).get('trigger') == trigger
                and scope['last_run']['status'] != 'running'
                for scope in result.values()
            ),
            f'{trigger} scope results',
        )

    write_config(config, {'login_result': 'manual', 'browser_url': verification})
    auth = call('connector', 'login', 'fixture')['id']
    browser_ready(auth)
    cancelled = call('connector', 'cancel-login', auth)
    require(cancelled['status'] == 'cancelled' and 'verification_url' not in cancelled,
            'Cancelled login retained browser state')
    require(call('connector', 'connection', 'list')['items'] == [], 'Cancellation created a connection')
    print('Cancellation ended the browser session and left zero connections.')

    write_config(config, {
        'login_result': 'manual', 'browser_url': verification, 'fail_second_page': 'OPS',
    })
    auth = call('connector', 'login', 'fixture')['id']
    browser = browser_ready(auth)
    with urllib.request.urlopen(browser['verification_url'], context=tls, timeout=5) as response:
        require(b'Authorize fixture account' in response.read(), 'Fixture browser form missing')
    approval = urllib.request.Request(browser['verification_url'], method='POST', data=b'')
    with urllib.request.urlopen(approval, context=tls, timeout=5) as response:
        require(response.status == 204, 'Fixture browser did not accept approval')
    account = wait_for(
        lambda: call('connector', 'auth-status', auth),
        lambda result: result['status'] != 'waiting', 'approved login',
    )
    require(account['status'] == 'authorized' and 'account_ref' not in account, 'Authorization did not complete')

    choices = call('connector', 'options', 'fixture', '--authorization', auth, '--field', 'project')
    second = call('connector', 'options', 'fixture', '--authorization', auth, '--field', 'project',
                  '--page', choices['next_page'])
    require([choices['items'][0]['value'], second['items'][0]['value']] == ['PAY', 'OPS'],
            'Scope discovery did not return both pages')
    body = {
        'authorization_id': auth, 'endpoint_schema_version': 1, 'scope_schema_version': 1,
        'scopes': [{'config': {'project': name}} for name in ('PAY', 'OPS', 'PAY')],
    }
    saved = call('connector', 'connection', 'save', '-', body=body)
    cid = saved['connection_id']
    first, second, duplicate = saved['scopes']
    require([first['status'], second['status'], duplicate['status']] ==
            ['queued', 'queued', 'duplicate_selection'], 'Repeated selection queued a duplicate')
    require(first['scope_id'] == duplicate['scope_id'], 'Duplicate selection returned a new scope ID')
    parents = call('connector', 'connection', 'list')['items']
    require(len(parents) == 1 and len(parents[0]['scopes']) == 2, 'Expected one account and two scopes')

    initial = scopes(cid, 'initial')
    require(initial['PAY']['last_run']['status'] == 'success'
            and initial['PAY']['last_run']['fetched'] == 2
            and initial['OPS']['last_run']['status'] == 'failed'
            and 'last_success' not in initial['OPS'], 'Initial scope results were not isolated')
    require(call('task', 'list')['total'] == 2, 'Partial OPS page became visible')
    print('One connection, two scopes, duplicate PAY reused its ID. PAY published 2 tasks and OPS failed on page 2.')

    repeated = call('connector', 'connection', 'save', '-', body=body)
    require(repeated['connection_id'] == cid and repeated['scopes'][1]['status'] == 'already_connected',
            'Repeated save created another binding')
    write_config(config, {})
    call('connector', 'connection', 'sync', cid)
    retry = scopes(cid, 'manual')
    require(retry['PAY']['last_run']['mode'] == 'delta' and retry['PAY']['last_run']['changed'] == 0
            and retry['OPS']['last_run']['mode'] == 'full' and retry['OPS']['last_run']['status'] == 'success',
            'Independent retry did not keep PAY and publish OPS')
    require(call('task', 'list')['total'] == 4, 'Expected two published tasks per scope')
    print('Repeated save reused the connection. Retry left PAY unchanged and published 2 OPS tasks.')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('candidate', type=Path, help='directory containing apilot, apilot-app and apilot-fixture')
    args = parser.parse_args()
    candidate = args.candidate.resolve()
    for name in ('apilot', 'apilot-app', 'apilot-fixture'):
        if not os.access(candidate / name, os.X_OK):
            parser.error(f'Candidate executable missing: {candidate / name}')

    with tempfile.TemporaryDirectory(prefix='apilot-uc01-') as temp:
        directory = Path(temp).resolve()
        data = directory / 'data'
        config = data / 'connectors' / 'fixture' / 'fixture.json'
        fixture = directory / 'apilot-fixture'
        shutil.copy2(candidate / fixture.name, fixture)
        manifest = directory / 'manifest.json'
        arch = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64'}[platform.machine()]
        manifest.write_text(json.dumps({
            'manifest_version': 1, 'connector_id': 'fixture', 'release_version': '0.1.0',
            'protocol_versions': [1], 'os': platform.system().lower(), 'arch': arch,
            'entrypoint': fixture.name, 'entrypoint_sha256': hashlib.sha256(fixture.read_bytes()).hexdigest(),
        }))
        env = dict(os.environ, APILOT_DATA_DIR=str(data), APILOT_DEV_CONNECTOR_MANIFEST=str(manifest))
        cert, key = directory / 'localhost.pem', directory / 'localhost.key'
        subprocess.run([
            'openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
            '-subj', '/CN=localhost', '-addext', 'subjectAltName=IP:127.0.0.1',
            '-keyout', str(key), '-out', str(cert),
        ], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        tls = ssl.create_default_context(cafile=cert)
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            port = listener.getsockname()[1]
        verification = f'https://127.0.0.1:{port}/fixture-login'

        with (directory / 'core.log').open('w+') as core_log, (directory / 'browser.log').open('w+') as browser_log:
            core = subprocess.Popen([candidate / 'apilot-app', '--serve'], env=env, stdout=core_log, stderr=core_log)
            try:
                wait_for(lambda: cli_call(candidate / 'apilot', env, 'ready', check=False), bool, 'Core readiness')
                browser = subprocess.Popen([
                    fixture, 'browser', '--listen', f'127.0.0.1:{port}', '--cert', cert,
                    '--key', key, '--data-dir', config.parent,
                ], stdout=browser_log, stderr=browser_log)
                try:
                    def listening():
                        try:
                            with socket.create_connection(('127.0.0.1', port), timeout=0.1):
                                return True
                        except OSError:
                            return False

                    wait_for(listening, bool, 'loopback fixture browser')
                    scenarios(candidate / 'apilot', env, config, verification, tls)
                finally:
                    stop(browser)
            except Exception:
                for log in (core_log, browser_log):
                    log.seek(0)
                    print(log.read(), end='')
                raise
            finally:
                stop(core)


if __name__ == '__main__':
    main()
