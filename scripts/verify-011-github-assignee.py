#!/usr/bin/env python3
"""Exercise GitHub assignee identity against a disposable loopback HTTPS server."""

import argparse
from contextlib import contextmanager
from datetime import datetime, timezone
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import selectors
import ssl
import subprocess
import tempfile
import threading
import urllib.request

TOKEN = 'controlled-no-key-token'
ISSUE_PATH = '/repositories/19/issues/1'


class GitHubFixture:
    def __init__(self):
        self.approved = False
        self.events = []
        self.issue = {
            'id': 42, 'number': 1, 'title': 'Verify assignee identity',
            'body': '', 'state': 'open', 'assignees': [], 'labels': [],
            'updated_at': '2026-11-23T00:00:00Z',
        }

    def handle(self, method, path, auth, body, origin):
        event = {'method': method, 'path': path}
        self.events.append(event)
        if (method, path) == ('POST', '/login/device/code'):
            return 200, {
                'device_code': 'local-device', 'user_code': 'LOCAL-TEST',
                'verification_uri': origin + '/login/device', 'expires_in': 60, 'interval': 1,
            }
        if (method, path) == ('GET', '/login/device'):
            return 200, {'fixture': 'loopback browser authorization'}
        if (method, path) == ('POST', '/login/device'):
            self.approved = True
            return 204, None
        if (method, path) == ('POST', '/login/oauth/access_token'):
            if not self.approved:
                return 200, {'error': 'authorization_pending'}
            return 200, {'access_token': TOKEN, 'token_type': 'bearer'}
        if auth != 'Bearer ' + TOKEN:
            return 401, {}
        if (method, path) == ('GET', '/user'):
            return 200, {'id': 7, 'login': 'fixture-reviewer'}
        if (method, path) == ('GET', '/user/installations'):
            return 200, {'installations': [{'id': 1, 'app_slug': 'apilot-app'}]}
        if (method, path) == ('GET', '/user/installations/1/repositories'):
            return 200, {'repositories': [{'id': 19, 'full_name': 'fixture/assignees', 'has_issues': True}]}
        if (method, path) == ('GET', '/repositories/19'):
            return 200, {'id': 19, 'has_issues': True, 'permissions': {'push': True}}
        if (method, path) == ('GET', '/repositories/19/issues'):
            return 200, [self.issue]
        if (method, path) == ('GET', '/users/octocat'):
            return 200, {'id': 777, 'login': 'octocat'}
        if (method, path) == ('GET', ISSUE_PATH):
            return 200, self.issue
        if (method, path) == ('PATCH', ISSUE_PATH):
            submitted = json.loads(body)
            event['assignees'] = submitted.get('assignees')
            if submitted != {'assignees': ['octocat']}:
                return 422, {}
            self.issue['assignees'] = [{'id': 777, 'login': 'octocat'}]
            self.issue['updated_at'] = datetime.now(timezone.utc).isoformat().replace('+00:00', 'Z')
            return 200, self.issue
        return 404, {}


@contextmanager
def start_fixture(directory):
    cert, key = directory / 'fixture.pem', directory / 'fixture.key'
    subprocess.run([
        'openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
        '-subj', '/CN=localhost', '-addext', 'subjectAltName=IP:127.0.0.1',
        '-keyout', str(key), '-out', str(cert),
    ], check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    fixture = GitHubFixture()

    class Handler(BaseHTTPRequestHandler):
        def serve(self):
            length = int(self.headers.get('Content-Length', 0))
            status, value = fixture.handle(
                self.command, self.path.split('?', 1)[0], self.headers.get('Authorization'),
                self.rfile.read(length), origin,
            )
            self.send_response(status)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            if value is not None:
                self.wfile.write(json.dumps(value).encode())

        do_GET = do_POST = do_PATCH = serve

        def log_message(self, *args):
            pass

    with ThreadingHTTPServer(('127.0.0.1', 0), Handler) as server:
        origin = f'https://127.0.0.1:{server.server_port}'
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(cert, key)
        server.socket = context.wrap_socket(server.socket, server_side=True)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            yield fixture, origin, cert
        finally:
            server.shutdown()
            thread.join()


def require(value, why):
    if not value:
        raise RuntimeError(why)


class ConnectorProbe:
    def __init__(self, binary, directory):
        self.command = [str(binary), 'serve', '--protocol', '1']
        self.env = dict(os.environ, APILOT_CONNECTOR_DATA_DIR=str(directory))

    @staticmethod
    def request(method, params):
        return (json.dumps({'jsonrpc': '2.0', 'id': method, 'method': method, 'params': params}) + '\n').encode()

    def call(self, method, params):
        result = subprocess.run(self.command, input=self.request(method, params), capture_output=True,
                                check=True, env=self.env, timeout=15)
        return self.terminal(result.stdout, method)

    @staticmethod
    def terminal(output, method):
        lines = output.splitlines()
        require(len(lines) == 1, f'{method}: expected one final response')
        response = json.loads(lines[0])
        require(response.get('id') == method and 'result' in response, f'{method}: {response}')
        return response['result']

    def login(self, origin, cert):
        with subprocess.Popen(self.command, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                              stderr=subprocess.PIPE, env=self.env) as process:
            try:
                process.stdin.write(self.request('auth.login', {}))
                process.stdin.close()
                process.stdin = None
                with selectors.DefaultSelector() as selector:
                    selector.register(process.stdout, selectors.EVENT_READ)
                    require(selector.select(5), 'No browser authorization notification')
                event = json.loads(process.stdout.readline())
                require(event.get('method') == 'open_browser', f'Unexpected login response: {event}')
                url = event['params']['url']
                require(url == origin + '/login/device', 'Browser authorization escaped the fixture')
                request = urllib.request.Request(url, data=b'', method='POST')
                client = urllib.request.build_opener(
                    urllib.request.ProxyHandler({}),
                    urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=cert)),
                )
                with client.open(request, timeout=4) as response:
                    require(response.status == 204, 'Browser fixture did not approve')
                stdout, stderr = process.communicate(timeout=15)
                require(process.returncode == 0, stderr.decode())
                return self.terminal(stdout, 'auth.login')
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()


def run(binary):
    with tempfile.TemporaryDirectory(prefix='apilot-github-assignee-') as temp:
        directory = Path(temp).resolve()
        setting = directory / 'controlled-github.json'
        probe = ConnectorProbe(binary, directory)
        require(subprocess.run(probe.command, input=probe.request('describe', {}), capture_output=True,
                               env=probe.env, timeout=5).returncode != 0,
                'Missing controlled configuration did not fail closed. Build with the controlled tag.')
        with start_fixture(directory) as (fixture, origin, cert):
            setting.write_text(json.dumps({'origin': 'https://github.com:443', 'root_pem': cert.read_text()}))
            require(subprocess.run(probe.command, input=probe.request('describe', {}), capture_output=True,
                                   env=probe.env, timeout=5).returncode != 0,
                    'A remote origin was accepted by the controlled Connector')
            setting.write_text(json.dumps({'origin': origin, 'root_pem': cert.read_text()}))
            description = probe.call('describe', {})
            require(description['connector_id'] == 'github-issues', 'Wrong Connector implementation')
            account = probe.login(origin, cert)
            require(account['account_ref'] == '7' and account['endpoint_ref'] == 'github.com', 'Wrong fixture account')
            options = probe.call('config.options', {
                'account_ref': account['account_ref'], 'endpoint_ref': account['endpoint_ref'],
                'field': 'repository_id',
            })
            require(options['items'] == [{'value': '19', 'label': 'fixture/assignees'}], 'Fixture discovery mismatch')
            scope = {
                'account_ref': '7', 'endpoint_ref': 'github.com', 'scope_ref': '19',
                'endpoint_config': {}, 'scope_config': {'repository_id': '19'},
                'endpoint_schema_version': 1, 'scope_schema_version': 1,
            }
            checked = probe.call('connection.validate', {**scope, 'mode': 'access'})
            require(checked['scope_ref'] == '19', 'Repository binding mismatch')
            record = probe.call('tasks.sync', {**scope, 'mode': 'full'})['records'][0]
            params = {
                **scope, 'source_id': record['source_id'], 'number': record['number'],
                'expected_version': record['source_version'],
            }
            metadata = probe.call('tasks.edit_metadata', params)
            require(any(field['id'] == 'assignees' and field['writable'] for field in metadata['fields']),
                    'Assignee editing missing')
            mutation = probe.call('tasks.mutate', {
                **params, 'operation_id': 'assign-by-login',
                'baseline': {'assignees': record['fields']['assignees']},
                'patch': {'assignees': {'type': 'accounts', 'value': [{'id': 'octocat', 'label': 'octocat'}]}},
            })
            require(mutation['status'] == 'confirmed', f'Unconfirmed assignee: {mutation}')
            confirmed = mutation['record']['fields']['assignees']
            require(confirmed['value'] == [{'id': '777', 'label': 'octocat'}], f'Incorrect stable ID: {confirmed}')
            rejected = probe.call('tasks.mutate', {
                **params, 'operation_id': 'assign-wrong-id',
                'baseline': {'assignees': confirmed},
                'patch': {'assignees': {'type': 'accounts', 'value': [{'id': '999', 'label': 'octocat'}]}},
            })
            require(rejected['status'] == 'not_executed', f'Mismatched ID was accepted: {rejected}')
            writes = [event for event in fixture.events if event['method'] == 'PATCH']
            require(writes == [{'method': 'PATCH', 'path': ISSUE_PATH, 'assignees': ['octocat']}],
                    f'Unexpected writes: {writes}')
            write_index = fixture.events.index(writes[0])
            lookup = {'method': 'GET', 'path': '/users/octocat'}
            read = {'method': 'GET', 'path': ISSUE_PATH}
            require(lookup in fixture.events[:write_index] and read in fixture.events[write_index+1:]
                    and fixture.events.count(lookup) == 2, 'Missing lookup, readback, or mismatched-ID verification')
            return {
                'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
                'connector': description['connector_id'], 'release_version': description['release_version'],
                'upstream': origin, 'confirmed_assignees': confirmed['value'],
                'wrong_id_status': rejected['status'], 'patch_count': len(writes),
                'events': fixture.events,
            }


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('connector', type=Path)
    args = parser.parse_args()
    print(json.dumps(run(args.connector.resolve()), indent=2))


if __name__ == '__main__':
    main()
