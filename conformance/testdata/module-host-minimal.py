#!/usr/bin/env python3
"""A complete Terra module, in the standard library only.

이 파일은 예시가 아니라 **시험 대상**이다. 통합 묶음의
`TestTheDocumentedHTTPContractIsEnoughForANonGoModule`이 이 파일을 그대로 띄우고
Terra의 실제 활성화 코드(IdentityIssuer.Issue -> Activate -> NextActivationState)로
붙는다. 계약이 움직이면 문서가 아니라 이 시험이 먼저 빨개진다.

계약은 [[docs/contracts/module-host-http-contract]]가 소유한다. 여기서 하는 일은
그 문서의 네 줄을 그대로 옮긴 것뿐이다:

  1. 환경에서 신원을 읽는다
  2. 배정받은 loopback endpoint에 묶는다
  3. 자격을 확인한 뒤 /terra/handshake 에서 instanceId·nonce를 되돌려준다
  4. /terra/readiness 에서 준비 상태를 답한다
"""

import json
import os
from http.server import BaseHTTPRequestHandler, HTTPServer

MODULE_ID = os.environ["TERRA_MODULE_ID"]
INSTANCE_ID = os.environ["TERRA_INSTANCE_ID"]
NONCE = os.environ["TERRA_BOOTSTRAP_NONCE"]
CREDENTIAL = os.environ["TERRA_WORKLOAD_CREDENTIAL"]
ENDPOINT = os.environ["TERRA_LOOPBACK_ENDPOINT"]

CREDENTIAL_HEADER = "X-Terra-Workload-Credential"


class Handler(BaseHTTPRequestHandler):
    def _send(self, status, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):  # noqa: N802  (stdlib naming)
        # 자격은 두 제어 경로와 모듈 자신의 경로 모두에 걸린다. 없는 요청은
        # 답을 보기 전에 끊는다 — endpoint는 loopback이지만 같은 기계의 다른
        # 프로세스는 여전히 남이다.
        if self.headers.get(CREDENTIAL_HEADER) != CREDENTIAL:
            self._send(401, {"error": "unauthorized"})
            return
        if self.path == "/terra/handshake":
            # 발급받은 instanceId·nonce를 그대로 되돌려주는 것이 "내가 방금
            # 띄워진 그 instance다"라는 증명 전부다.
            self._send(200, {"instanceId": INSTANCE_ID, "nonce": NONCE})
            return
        if self.path == "/terra/readiness":
            self._send(200, {"ready": True})
            return
        if self.path == "/api/modules/" + MODULE_ID + "/v1/status":
            self._send(200, {"status": "ok", "version": "0.1.0"})
            return
        self._send(404, {"error": "not found"})

    def log_message(self, *_args):
        pass  # 호스트가 stdout을 읽으므로 접근 로그는 내지 않는다.


def main():
    host, port = ENDPOINT.rsplit(":", 1)
    server = HTTPServer((host, int(port)), Handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
