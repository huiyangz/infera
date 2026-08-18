#!/usr/bin/env python3
"""R11 http 形态契约验证 stub（infera 侧工具，非任何产品的替身）。

用途：验证 infera http runner 的线上契约——POST {role,prompt,workdir} → 200 {output}。
把 codeReviewAgent（或任一 agent）注册为 http 形态指向本 stub，跑一次交付，
stub 落盘收到的每个请求（method/path/headers/body），即可拿到产品必须实现的
精确请求/响应形状。响应带 infera-findings fenced block，顺带验证 R10 findings 解析。

启动：python3 http-contract-stub.py [port]   （默认 18090；请求日志打到 stdout）
"""
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18090


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(n).decode("utf-8", "replace")
        try:
            req = json.loads(body)
        except ValueError:
            req = {"_raw": body}
        # 请求侧证据：method/path/headers/body 一行一条 JSON
        print(json.dumps({
            "path": self.path,
            "headers": dict(self.headers),
            "body": req,
        }, ensure_ascii=False), flush=True)
        role = req.get("role", "?")
        # 响应侧契约：{"output": "..."}；带合法 findings 块验证 R10 解析
        output = (
            f"[http-contract-stub] role={role} prompt_len={len(req.get('prompt', ''))} "
            f"workdir={req.get('workdir', '')}\n"
            "```infera-findings\n[]\n```"
        )
        payload = json.dumps({"output": output}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *_):  # 静音默认访问日志（证据只走上面的 JSON 行）
        pass


HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
