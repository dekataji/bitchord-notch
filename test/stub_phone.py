# Stub of the BitChord phone web-remote API for Wine sandbox tests.
# Logs every request (method, path, body) to stub.log next to this file.
import json, os, threading
from http.server import BaseHTTPRequestHandler, HTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
LOG = os.path.join(HERE, "stub.log")

STATE = {
    "connected": True, "playing": True, "loading": False,
    "positionMs": 61000, "durationMs": 213000,
    "index": 1, "queueSize": 2, "shuffle": False, "repeat": 1,
    "hasNext": True, "hasPrevious": True, "volume": 9, "volumeMax": 15,
    "current": {"videoId": "v2", "title": "Second Song", "artist": "Artist B",
                "thumbnailUrl": None, "durationText": "3:33"},
    "queue": [
        {"videoId": "v1", "title": "First Song", "artist": "Artist A",
         "thumbnailUrl": None, "durationText": "3:01", "queuedBy": "Irene"},
        {"videoId": "v2", "title": "Second Song", "artist": "Artist B",
         "thumbnailUrl": None, "durationText": "3:33"},
    ],
    "users": ["Irene"],
}

LYRICS = {"videoId": "v2", "found": True, "synced": True, "source": "LRCLIB",
          "lines": [{"at": 0, "text": "line one"},
                    {"at": 5000, "text": "line two"},
                    {"at": 65000, "text": "line three"}]}

def log(msg):
    with open(LOG, "a") as f:
        f.write(msg + "\n")

class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _auth(self):
        return self.headers.get("X-Remote-Token") == "sess1"

    def do_POST(self):
        n = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(n).decode("utf-8", "replace") if n else ""
        log(f"POST {self.path} {body}")
        if self.path == "/api/unlock":
            data = json.loads(body or "{}")
            if data.get("pin") == "1234":
                self._json(200, {"token": "sess1", "name": data.get("name", "")})
            else:
                self._json(401, {"error": "wrong PIN"})
            return
        if self.path == "/api/command":
            if not self._auth():
                self._json(401, {"error": "locked"})
                return
            self._json(200, {"ok": True})
            return
        self._json(404, {"error": "not found"})

    def do_GET(self):
        log(f"GET {self.path}")
        if self.path == "/api/state":
            if not self._auth():
                self._json(401, {"error": "locked"})
                return
            self._json(200, STATE)
            return
        if self.path == "/api/lyrics":
            if not self._auth():
                self._json(401, {"error": "locked"})
                return
            self._json(200, LYRICS)
            return
        self._json(404, {"error": "not found"})

if __name__ == "__main__":
    log("stub started")
    HTTPServer(("127.0.0.1", 8899), H).serve_forever()
