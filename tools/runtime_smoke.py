"""Exercise a locally built binary against temporary synthetic loopback services."""
import argparse
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import threading
import time
import urllib.error
import urllib.request

CLIENT = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=Path('bin/inputcheck'))
    args = parser.parse_args()
    binary = args.binary.resolve()
    assert binary.is_file(), 'Build the local binary first'
    client_token, upstream_token = secrets.token_urlsafe(24), secrets.token_urlsafe(24)
    seen, blocked, release = [], threading.Event(), threading.Event()
    should_block = threading.Event()

    class Upstream(BaseHTTPRequestHandler):
        def do_POST(self):
            assert self.path == '/v1/chat/completions'
            assert self.headers.get('Authorization') == 'Bearer ' + upstream_token
            assert not self.headers.get('Cookie')
            assert not self.headers.get('X-Api-Key')
            body = self.rfile.read(int(self.headers['Content-Length']))
            seen.append(body)
            if should_block.is_set():
                blocked.set()
                assert release.wait(5), 'Synthetic upstream was not released'
            self.send_response(200)
            self.send_header('Content-Type', 'text/event-stream')
            self.send_header('Set-Cookie', 'must-not-reach-client=1')
            self.end_headers()
            self.wfile.write(b'data: {"choices":[{"delta":{"content":"synthetic"}}]}\n\ndata: [DONE]\n\n')
        def log_message(self, *args):
            pass

    upstream = ThreadingHTTPServer(('127.0.0.1', 0), Upstream)
    thread = threading.Thread(target=upstream.serve_forever, daemon=True)
    thread.start()
    processes = []

    @contextmanager
    def service(mode='inspect', proxy=False, strict=False):
        with tempfile.TemporaryDirectory(prefix='inputcheck-smoke-') as directory:
            with socket.socket() as sock:
                sock.bind(('127.0.0.1', 0))
                port = sock.getsockname()[1]
            config = {'listen': f'127.0.0.1:{port}', 'mode': mode,
                      'gateway_token_env': 'SYNTHETIC_CLIENT_TOKEN', 'reject_warnings': strict,
                      'timeout_seconds': 5, 'shutdown_seconds': 6}
            if proxy:
                config.update(upstream=f'http://127.0.0.1:{upstream.server_port}',
                              upstream_token_env='SYNTHETIC_UPSTREAM_TOKEN', models={'chat-a': 'chat-a'})
            path = Path(directory) / 'config.json'
            path.write_text(json.dumps(config))
            env = {**os.environ, 'SYNTHETIC_CLIENT_TOKEN': client_token, 'SYNTHETIC_UPSTREAM_TOKEN': upstream_token}
            proc = subprocess.Popen([str(binary), 'serve', '-config', str(path)], env=env,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            processes.append(proc)
            base = f'http://127.0.0.1:{port}'
            try:
                for _ in range(100):
                    try:
                        with CLIENT.open(base + '/healthz', timeout=.2) as response:
                            if response.status == 200:
                                break
                    except (OSError, urllib.error.URLError):
                        if proc.poll() is not None:
                            raise AssertionError('Temporary service exited')
                        time.sleep(.03)
                else:
                    raise AssertionError('Temporary service did not become ready')
                yield proc, base
            finally:
                release.set()
                if proc.poll() is None:
                    proc.terminate()
                out, error = proc.communicate(timeout=8)
                assert proc.returncode == 0, 'Temporary service shutdown failed'
                for marker in [client_token, upstream_token, 'SYNTHETIC_PRIVATE_NOTE']:
                    assert marker.encode() not in out + error, 'Body or credential leaked to logs'

    def post(base, route, body, authorized=True):
        headers = {'Content-Type': 'application/json', 'Cookie': 'client-secret=1', 'X-Api-Key': 'client-secret'}
        if authorized:
            headers['Authorization'] = 'Bearer ' + client_token
        request = urllib.request.Request(base + route, data=body, headers=headers)
        try:
            response = CLIENT.open(request, timeout=8)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, response.read(), response.headers

    valid = b'{ "model":"chat-a", "messages":[{"role":"user","content":"<input>SYNTHETIC_PRIVATE_NOTE</input>"}], "stream":true }'
    warning = b'{"model":"chat-a","messages":[{"role":"user","content":"<input>unclosed"}]}'
    try:
        with service() as (_, base):
            status, data, _ = post(base, '/v1/check', valid)
            assert status == 200 and json.loads(data)['valid'] and b'SYNTHETIC_PRIVATE_NOTE' not in data
            assert post(base, '/v1/check', valid, authorized=False)[0] == 401
            assert post(base, '/v1/check', b'{bad')[0] == 400
            assert post(base, '/v1/chat/completions', valid)[0] == 404
            assert not seen
        with service(proxy=True, strict=True) as (proc, base):
            assert post(base, '/v1/chat/completions', warning)[0] == 422
            assert not seen
            status, data, headers = post(base, '/v1/chat/completions', valid)
            assert status == 200 and b'[DONE]' in data and seen[-1] == valid
            assert headers.get('X-InputCheck-Mode') == 'inspect' and not headers.get('Set-Cookie')
            release.clear()
            blocked.clear()
            should_block.set()
            result = []
            worker = threading.Thread(target=lambda: result.append(post(base, '/v1/chat/completions', valid)))
            worker.start()
            assert blocked.wait(3)
            proc.terminate()
            time.sleep(.15)
            assert proc.poll() is None, 'In-flight request was forcibly closed'
            release.set()
            worker.join(timeout=5)
            assert not worker.is_alive() and result and b'[DONE]' in result[0][1]
            proc.wait(timeout=5)
        should_block.clear()
        with service(mode='encode', proxy=True) as (_, base):
            assert post(base, '/v1/chat/completions', valid)[0] == 200
            text = json.loads(seen[-1])['messages'][0]['content']
            assert '[[OBF-B64/1' in text and 'SYNTHETIC_PRIVATE_NOTE' not in text
            before = len(seen)
            assert post(base, '/v1/chat/completions', warning)[0] == 422 and len(seen) == before
        print('PASS: check-only isolation, auth, schema rejection, strict gate, unchanged proxy, SSE, separate credentials, header stripping, graceful drain, explicit encoding, metadata-only logs')
    finally:
        release.set()
        for proc in processes:
            if proc.poll() is None:
                proc.terminate()
                proc.wait(timeout=8)
        upstream.shutdown()
        upstream.server_close()
        thread.join()


if __name__ == '__main__':
    main()
