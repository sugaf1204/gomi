#!/usr/bin/env python3
import argparse
import json
import time
from http.server import BaseHTTPRequestHandler, HTTPServer


class Handler(BaseHTTPRequestHandler):
    def _write(self, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length > 0 else b"{}"
        try:
            payload = json.loads(raw.decode("utf-8"))
        except Exception:
            payload = {"raw": raw.decode("utf-8", errors="replace")}

        event = {
            "timestamp": time.time(),
            "path": self.path,
            "payload": payload,
        }
        with open(self.server.log_file, "a", encoding="utf-8") as f:
            f.write(json.dumps(event, ensure_ascii=True) + "\n")

        self._write({"status": "ok"})

    def log_message(self, fmt, *args):
        return


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--listen", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=19090)
    parser.add_argument("--log-file", required=True)
    args = parser.parse_args()

    httpd = HTTPServer((args.listen, args.port), Handler)
    httpd.log_file = args.log_file
    print(f"mock power server listening on {args.listen}:{args.port}")
    httpd.serve_forever()


if __name__ == "__main__":
    main()
