# CI-only HTTP: no connection to Google; no captured payload/token logging.
import json
from http.server import BaseHTTPRequestHandler, HTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        self.rfile.read(int(self.headers.get("Content-Length", "0")))
        response = {"access_token": "disposable-smoke-token", "expires_in": 3600, "token_type": "Bearer"} if self.path == "/token" else {"name": "projects/p0-smoke/messages/probe"}
        raw = json.dumps(response).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)
    def log_message(self, *_):
        pass

HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
