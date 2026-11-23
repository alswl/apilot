#!/usr/bin/env python3
"""Run the UC01 Core/CLI checks against the local protocol fixture and a disposable database."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import selectors
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
    return cid, first['scope_id'], second['scope_id']




def sync_invalidation(cli, env, request_sync):
    watcher = subprocess.Popen([str(cli), '--json', 'workbench', 'watch'],
                               env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(watcher.stdout, selectors.EVENT_READ)
            pending = b''

            def next_event():
                nonlocal pending
                deadline = time.monotonic() + 10
                while b'\n' not in pending:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0 or not selector.select(remaining):
                        raise RuntimeError('Missing SSE event from the candidate')
                    chunk = os.read(watcher.stdout.fileno(), 4096)
                    if not chunk:
                        raise RuntimeError('Candidate SSE stream closed unexpectedly')
                    pending += chunk
                line, pending = pending.split(b'\n', 1)
                return json.loads(line)['data']

            initial = next_event()
            require(initial['type'] == 'snapshot', 'Initial SSE snapshot missing')
            request_sync()
            event = next_event()
            require(event['type'] == 'changed' and event['instance'] == initial['instance']
                    and event['sequence'] > initial['sequence'], 'SSE invalidation mismatch')
    finally:
        stop(watcher)
        watcher.stdout.close()
        watcher.stderr.close()

def remaining_scenarios(cli, env, config, connection_id, pay, ops):
    def call(*args, **kwargs):
        return cli_call(cli, env, *args, **kwargs)

    items = call('task', 'list', '--scope', pay, '--source', 'fixture', '--assignee', 'Alice',
                 '--priority', '2', '--sort', 'due_date', '--descending')['items']
    require(len(items) == 2 and items[0]['native_id'] == 'PAY-1' and 'due_date' not in items[1],
            'UC07 task projection/filter/sort mismatch')
    task_id = items[0]['source_object_id']
    print('UC07 scope, source, assignee, priority and due-date sorting verified.')

    detail = call('task', 'detail', task_id, '--segment', 'comments')
    more = call('task', 'detail', task_id, '--segment', 'comments', '--page', detail['next_page'])
    require(more['items'][0]['id'] == 'comments-2', 'UC09 detail pagination mismatch')
    print('UC09 detail returned both comment pages.')

    mapping = {
        'revision': 0, 'task_id': task_id,
        'rules': {'due_date': {'field': 'promised', 'write': True}},
    }
    preview = call('connection', 'mapping', 'preview', pay, '-', body=mapping)
    require(preview['sample']['due_date'] == '2026-12-01' and 'issues' not in preview, 'UC11 mapping preview mismatch')
    require(call('connection', 'mapping', 'save', pay, '-', body=mapping)['revision'] == 1,
            'UC11 mapping revision mismatch')
    task = wait_for(lambda: call('task', 'show', task_id),
                    lambda value: value.get('due_date') == '2026-12-01', 'reprojected PAY task')
    require(call('task', 'list', '--scope', ops, '--sort', 'due_date')['items'][0]['due_date'] == '2026-12-15',
            'UC11 mapping affected another scope')
    print('UC11 preview and save reprojected PAY, preserving task identity and OPS dates.')

    connection = call('connection', 'show', pay)
    updated = call('connection', 'save', '-', body={
        'id': pay, 'kind': 'fixture', 'name': connection['name'],
        'version': connection['version'], 'write_enabled': True,
    })
    require(updated['write_enabled'], 'UC13 scope write enablement failed')
    metadata = call('task', 'edit-metadata', task_id)
    require(any(field['id'] == 'title' for field in metadata['fields']), 'UC13 editable title missing')

    def operation(request_id, wanted, **intent):
        current = call('task', 'show', task_id)
        command = {
            'request_id': request_id, 'connection_id': pay,
            'connection_version': current['connection_version'],
            'source_object_id': task_id, 'target_version': current['target_version'],
            'mapping_revision': 1, **intent,
        }
        call('task', 'operation', 'submit', '-', body=command)
        return wait_for(
            lambda: call('task', 'operation', 'show', request_id),
            lambda value: value['sync_state'] == wanted, f'{request_id} to become {wanted}',
        )

    write_config(config, {'write_result': 'lost'})
    operation('uc13-lost-reply', 'needs_review', fields={'title': {'type': 'text', 'value': 'Confirmed edit'}})
    require(call('task', 'show', task_id)['title'] == task['title'], 'UC13 pending write changed displayed title')
    require(call('task', 'operation', 'reconcile', 'uc13-lost-reply')['sync_state'] == 'confirmed'
            and call('task', 'show', task_id)['title'] == 'Confirmed edit', 'UC13 readback did not settle')
    print('UC13 lost-reply edit preserved the old title and settled on readback.')

    write_config(config, {})
    operation('uc14-resolution', 'confirmed', action='resolve',
              action_fields={'resolution': {'type': 'text', 'value': 'completed'}})
    require(call('task', 'show', task_id)['state'] == 'done', 'UC14 state not confirmed')
    operation('uc14-reopen', 'confirmed', action='reopen')
    require(call('task', 'show', task_id)['state'] == 'open', 'UC14 task not reopened')
    print('UC14 completed and reopened the fixture task through source actions.')

    write_config(config, {'rate_limited_scope': 'OPS'})
    sync_invalidation(cli, env, lambda: call('sync', 'run', '--connection', connection_id))
    log = wait_for(lambda: call('sync', 'list', '--scope', ops, '--status', 'failed')['items'],
                   lambda rows: rows and rows[0].get('error_kind') == 'rate_limited', 'OPS rate limit')[0]
    detail = call('sync', 'inspect', log['sync_run_id'])
    require(detail['sync_run_id'] == log['sync_run_id'] and detail.get('retry_at'), 'UC18 run detail mismatch')
    require(call('task', 'list', '--scope', pay)['total'] == 2, 'UC18 OPS rate limit affected PAY')
    print(f"UC18 inspected OPS run {log['sync_run_id']} and its retry time.")

    usage = call('sync', 'usage', '--scope', ops, '--status', 'failed')['items']
    limited = next(row for row in usage if row['mode'] == log['mode'])
    require(limited['rate_limits'] == 1 and limited['requests'] == detail['requests']
            and limited['scope_id'] == detail['scope_id'], 'UC19 usage and sync log disagree')
    print('UC19 filtered usage reconciled with the same run. SSE invalidation and API/CLI re-read verified.')

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('candidate', type=Path, help='directory containing apilot, apilot-app and apilot-fixture')
    parser.add_argument('--remaining', action='store_true', help='also check UC07, UC09, UC11, UC13, UC14, UC18 and UC19')
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
                    target = scenarios(candidate / 'apilot', env, config, verification, tls)
                    if args.remaining:
                        remaining_scenarios(candidate / 'apilot', env, config, *target)
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
