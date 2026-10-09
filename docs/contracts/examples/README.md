---
title: "최소 모듈 예제 (Python)"
doc_type: "guide"
scope: "project"
target: "terra-sdk"
status: "draft"
last_updated: "2026-10-09"
related:
  - "[[docs/contracts/module-host-http-contract]]"
  - "[[docs/guide/conformance]]"
---

# 최소 모듈 예제 (Python)

[`module-host-minimal.py`](module-host-minimal.py)는 표준 라이브러리만 쓴 71줄짜리 완전한 Terra 모듈이다.
Terra 레포의 `docs/contracts/examples/module-host-minimal.py`(origin/main `b2a76a6`)를 **그대로** 가져왔다.

이 파일은 예시이면서 **시험 대상**이다. 이 레포의 `conformance` 테스트가 이 파일을 그대로 띄워
[[docs/guide/conformance|적합성 테스트 키트]]를 통과시킨다. 계약이 움직이면 문서가 아니라 그 테스트가 먼저 빨개진다.

## 하는 일

[[docs/contracts/module-host-http-contract|계약 문서]]의 네 줄을 그대로 옮긴 것이다.

1. 환경변수 6개에서 신원을 읽는다 (`TERRA_MODULE_ID`, `TERRA_INSTANCE_ID`, `TERRA_BOOTSTRAP_NONCE`, `TERRA_WORKLOAD_CREDENTIAL`, `TERRA_LOOPBACK_ENDPOINT`; `TERRA_ACTIVATION_EPOCH`는 이 예제가 쓰지 않는다)
2. 배정받은 loopback endpoint에 묶는다
3. 모든 요청에서 `X-Terra-Workload-Credential`을 확인하고, `GET /terra/handshake`에서 발급받은 `instanceId`·`nonce`를 되돌려준다
4. `GET /terra/readiness`에서 `{"ready": true}`를 답한다

## 직접 돌려 보기 (Linux)

호스트 없이도 키트가 호스트 역할을 한다.

```sh
go run ./cmd/terra-conformance \
  -operation /api/modules/io.terra.conformance.sample/v1/status \
  -- python3 docs/contracts/examples/module-host-minimal.py
```

모든 줄이 `PASS`이고 종료 코드가 0이면 이 예제는 계약을 지킨다. 자기 모듈을 검사하려면 `--` 뒤의 명령만 바꾼다.
Windows에서는 `python3` 대신 `python`을 쓴다.

## 알려진 주의

이 예제는 `http.server.HTTPServer`를 쓰는데, 이 클래스는 bind와 listen 사이에 `socket.getfqdn()`을 부른다.
역방향 조회가 느린 환경(macOS CI 러너에서 20초를 넘긴 적이 있다고 의심한다 — 원인은 아직 확정하지 않았다)에서는
listen이 그만큼 늦어져 키트의 `-bind-timeout`(기본 20초)에 걸릴 수 있다. 그럴 때는 `-bind-timeout 90s`처럼 늘린다.
