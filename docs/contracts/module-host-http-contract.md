---
title: "모듈 측 HTTP 계약 — 호스트가 모듈에게 요구하는 전부"
doc_type: "contract"
scope: "module-platform"
target: "terra-module-runtime"
status: "active"
version: "1.1.0"
last_updated: "2026-09-28"
language: "ko-KR"
measured_at: "main 392236dd · windows-amd64 · 2026-09-21"
related:
  - "[[docs/modules/terra-common-module-runtime-gateway-detailed-design|공통 Module Runtime·Gateway 상세 설계]] §12·§15"
  - "[[docs/manual/06-usage/modules-develop|모듈 개발 (사용 설명서)]]"
  - "[[docs/reports/scriba-terra-module-feasibility-2026-09-15|Scriba 모듈화 판정서]] §2-1"
  - "[[docs/requirements/backlog/module-development-open-decisions|모듈 개발 잔여 결정]] M-1"
  - "[[products/common/packages/terra-module-sdk/README|terra-module-sdk README]]"
  - "[[docs/contracts/README|Contracts]]"
copied_from: "StellaxiaLab/Terra docs/contracts/module-host-http-contract.md"
copied_at_commit: "b2a76a6d3f6c2d4e8a21fddc05649b95d0be6f58"
copy_status: "원본 사본 — 본문 무변경"
---

# 모듈 측 HTTP 계약

> **이 파일은 사본이다.** 원본은 비공개 레포 `StellaxiaLab/Terra`의 `docs/contracts/module-host-http-contract.md`
> (origin/main `b2a76a6`)이고, 본문은 바꾸지 않았다. 아래 `[[...]]` 링크와 §10 표의 소스 경로는 Terra 안의
> 문서·파일을 가리키므로 이 레포에서는 풀리지 않는다. 이 레포에서의 대응은 [[docs/reference/provenance|출처 대응표]]를,
> 이 계약을 실제로 검사하는 도구는 [[docs/guide/conformance|적합성 테스트 키트]]를 본다.
> 문서와 코드가 어긋나면 코드가 맞다는 원칙(§10)은 그대로이고, 이 레포에서 "코드"는 `modulert`·`modulesdk` 패키지다.
>
> 원본이 머리말 인용문에서 Python 예시를 "63줄"이라 적었지만 현재 `examples/module-host-minimal.py`는 71줄이다(설계 문서 §3도 71줄).
> 원본 수정은 Terra 쪽 일이라 여기서는 고치지 않았다.

**모듈이 되기 위해 구현해야 하는 것의 전부**를 적는다. Go로 만들면
`terra-module-sdk`가 이것을 대신 해 주고, 다른 언어로 만들면 이 문서만 있으면 된다.

> **왜 SDK를 언어마다 내지 않나** — 낼 만큼 크지 않기 때문이다. stdlib만 쓴 Python
> 모듈 **63줄**이 Terra의 실제 활성화 코드를 통과해 `ready`에 도달하고 자기 UI origin까지
> 게시한 기록이 있다([[docs/reports/scriba-terra-module-feasibility-2026-09-15|Scriba 판정서]] §2-1).
> SDK가 없어서 못 만드는 것이 아니라 계약이 이만하다. 결정과 근거는
> [[docs/requirements/backlog/module-development-open-decisions|잔여 결정]] M-1,
> 결정 기록은 상세 설계 §28에 있다.

이 문서는 **제어 평면**만 소유한다. 모듈이 내는 Operation의 형식은
[[docs/contracts/api/terra-api-contract-standard-detailed-design|Terra API Contract 표준]]이,
패키지 형식은 Manifest v2 JSON Schema가 소유한다.

## 1. 한 장 요약

```text
호스트                                   모듈 프로세스
  │  launch + 환경변수 6개 주입              │
  ├────────────────────────────────────────>│  ① 환경에서 신원을 읽는다
  │                                         │  ② TERRA_LOOPBACK_ENDPOINT에 묶는다
  │  GET /terra/handshake  (+자격 헤더)      │
  ├────────────────────────────────────────>│  ③ instanceId·nonce를 되돌려준다
  │  GET /terra/readiness  (+자격 헤더)      │
  ├────────────────────────────────────────>│  ④ {"ready": true}
  │                                         │
  │  ⑤ route 게시 → 이후 호출이 이 endpoint로 들어온다
```

네 가지가 전부다. 나머지(권한·라우팅·감사·재시작)는 호스트의 일이다.

## 2. 환경변수 — 호스트가 launch마다 새로 발급한다

| 변수 | 뜻 | 없으면 |
| --- | --- | --- |
| `TERRA_MODULE_ID` | 이 프로세스가 대표하는 모듈 id | 신원 없음 |
| `TERRA_INSTANCE_ID` | **이번 실행**의 instance id | 신원 없음 |
| `TERRA_ACTIVATION_EPOCH` | 활성화 세대(부호 없는 정수). 옛 세대의 프로세스를 가려낸다 | 신원 없음 |
| `TERRA_BOOTSTRAP_NONCE` | handshake에서 되돌려줄 값 | 신원 없음 |
| `TERRA_WORKLOAD_CREDENTIAL` | 모든 요청에 요구할 자격 문자열 | 신원 없음 |
| `TERRA_LOOPBACK_ENDPOINT` | 묶을 `127.0.0.1:<port>` | 묶을 곳 없음 |

선택적으로 더 들어오는 것:

| 변수 | 언제 |
| --- | --- |
| `TERRA_MODULE_HOST_ENDPOINT` | 호스트가 Core capability를 제공할 때. 모듈이 여기로 `POST /terra/core/invoke` 한다 |
| `TERRA_MODULE_DATA_DIR` | `permissions.storage: ["module-data"]`를 선언했을 때 |
| `TERRA_MODULE_SHARED_ROOTS` | `permissions.storage: ["shared-roots"]` + **`built-in` 이상 신뢰 등급**일 때. `[{name,path}]` JSON |

> **매 실행마다 새로 발급된다.** 같은 모듈이라도 재시작하면 instance id·nonce·자격·포트가
> 전부 바뀐다. 어디에도 캐시하지 않는다 — 특히 자격을 파일에 적으면 다음 실행의 자격과
> 어긋난다.
>
> **포트는 매니페스트에 적지 않는다.** 호스트가 임시 포트를 배정하는 이유는 충돌 회피가
> 아니라 **모듈이 자기 주소를 고를 권한이 없기 때문**이다(설계 §15.4).

## 3. 자격 — 두 제어 경로와 모듈 자신의 경로 모두에 건다

```text
X-Terra-Workload-Credential: <TERRA_WORKLOAD_CREDENTIAL>
```

값이 다르거나 없으면 **401**로 답하고 본문을 만들지 않는다. endpoint가 loopback이라는
것은 같은 기계의 다른 프로세스를 막아 주지 않는다.

Gateway가 이 모듈로 호출을 forward할 때 같은 헤더를 붙인다(안 A). 그래서 모듈은
"제어 경로는 자격, 업무 경로는 무방비" 같은 구분을 두지 않는다 — **전부 건다.**

## 4. `GET /terra/handshake`

호스트가 프로세스를 띄운 직후 부른다.

**응답 200**

```json
{
  "instanceId": "<TERRA_INSTANCE_ID 그대로>",
  "nonce": "<TERRA_BOOTSTRAP_NONCE 그대로>",
  "contributionHash": "<선택>",
  "windowOrigin": "<선택>"
}
```

| 필드 | 규칙 |
| --- | --- |
| `instanceId` · `nonce` | **발급받은 값과 정확히 같아야 한다.** 다르면 호스트는 `identity handshake mismatch`로 실패시키고 route를 게시하지 않는다. 이것이 "내가 방금 띄워진 그 instance다"라는 증명 전부다 |
| `contributionHash` | 모듈의 기여 스냅샷 digest. 호스트가 route 게시에 함께 기록한다. 없으면 생략 |
| `windowOrigin` | 창 모드 GUI 앱이 **자기가 연** UI 리스너의 origin(`http://127.0.0.1:<port>`). 그 주소를 아는 것은 모듈뿐이라 매니페스트가 아니라 여기로 온다. Gateway가 loopback인지 검사한 뒤에야 믿는다 |

## 5. `GET /terra/readiness`

handshake가 성공한 뒤 부른다. 매니페스트의 `readiness.timeoutMs`(기본 15초) 안에 답해야 한다.

**응답 200**

```json
{ "ready": true }
```

준비되지 않았으면 `{"ready": false, "detail": "왜"}`. 그러면 호스트는 **route를 게시하지
않는다** — 반쯤 뜬 모듈에 호출자가 닿지 않게 하는 것이 이 계약의 일이다. `detail`은
사람이 읽는 줄이고, 그대로 상태에 실려 `terra daemon module get`에 보인다.

> 매니페스트가 `readiness`를 선언하지 않으면 handshake 성공만으로 ready가 된다.
> 선언할 때 `operationId`는 이 probe가 대표하는 Operation을 가리키고, `versionRange`는
> **그 Operation의 버전** 범위다(설계 §28 / [[docs/requirements/backlog/module-development-open-decisions|잔여 결정]] Q-4).

## 6. 그 밖의 경로 (선택)

| 경로 | 방향 | 언제 |
| --- | --- | --- |
| `GET /terra/svi/resources` | 호스트 → 모듈 | `contributions.svi.resources`를 선언했을 때. 노드 카탈로그로 정규화될 자원 서술자 배열을 답한다 |
| `POST /terra/svi/sink` | 호스트 → 모듈 | `contributions.svi.sink`를 선언했을 때. binding의 목적지가 된다. 본문의 `op`가 `open`(자원·endpoint에 sink를 열고 `sinkId`를 답한다. 자기 것이 아니면 404)·`write`(순서대로 한 프레임)·`close`(연 sink마다 정확히 한 번) 중 하나다. 거절 사유는 `detail`에 싣는다 |
| `POST /terra/core/invoke` | **모듈 → 호스트** | 모듈이 Core capability를 부를 때. 주소는 `TERRA_MODULE_HOST_ENDPOINT`, 자격은 같은 것. 브로커가 `permissions.coreOperations` 선언을 게이트한다 |
| 모듈 자신의 Operation 경로 | Gateway → 모듈 | 계약의 `bindings`가 적은 method·path 그대로. **다르면 404다** — 게시된 route는 맞는데 답이 없는 상태가 된다 |

## 7. 호출자 헤더 — 업무 경로로 오는 것

Gateway가 모듈의 Operation 경로로 호출을 넘길 때 다음 헤더를 붙인다. 클라이언트가 보낸
`X-Terra-*`는 Gateway 입구에서 지우므로 이 값은 **Gateway의 단언**이다. 사용자의 토큰은
모듈에 오지 않는다.

| 헤더 | 값 | 언제 |
| --- | --- | --- |
| `X-Terra-Workload-Credential` | 이 요청이 호스트에서 왔다는 증명(§3) | 항상 |
| `X-Terra-Principal` | 감사용 주체. 앱·에이전트 자격으로 온 호출이면 `"<user> via <delegate>"` | 주체가 있을 때 |
| `X-Terra-Subject` | 위임을 뺀 주체 — 호출이 대신하는 사람(또는 노드). **사람별 데이터의 키** | Principal이 있을 때 |
| `X-Terra-Delegate` | 거쳐 온 앱 id 또는 에이전트 자격 id | 위임된 호출일 때만 |
| `X-Terra-Grants` | 호출자의 유효 권한(공백 구분). 무제한이면 `*`. 로그인한 사람의 호출에는 신원 전용 권한 `session.identity`가 함께 실린다 | 권한이 있을 때 |
| `X-Terra-Trace-Id` | Gateway가 발급한 trace id | 로컬 경로 |

> **사람별로 데이터를 나누려면 `X-Terra-Subject`를 쓴다.** `X-Terra-Principal`은 감사가
> 읽는 모양이라 같은 사람이라도 어느 앱에서 왔는지에 따라 값이 다르다. 모듈이 로그인을 따로
> 만들 필요는 없다.

원격 경로(다른 노드의 모듈을 Master relay로 부를 때)에서는 Master가 세션으로 주체를 다시
푼다. 그래서 `X-Terra-Subject`는 `X-Terra-Principal`과 같고 `X-Terra-Delegate`는 없다.

### 7.1 함께 넘어오는 것과 돌아가는 것

호출자의 요청 헤더 중 넘어오는 것은 정해져 있다. 나머지(쿠키 포함)는 버려진다.

| 방향 | 헤더 |
| --- | --- |
| 요청 → 모듈 | `Content-Type`(본문이 있을 때)·`Accept`·`Accept-Language`·`If-None-Match`·`Range` |
| 모듈 → 응답 | 상태·본문·`Content-Type`, 그리고 `Cache-Control`·`Content-Disposition`·`Content-Range`·`ETag` |

`Set-Cookie`, CORS 헤더, 그 밖에 모듈이 내는 헤더는 호출자에게 가지 않는다. 요청 본문은
**1 MiB**까지다. 넘으면 Gateway가 413 `REQUEST_TOO_LARGE`로 거절하고 모듈은 호출되지
않는다. 잘라서 넘기지 않는다. 원격 경로는 본문과 결과만 나른다. 위 표는 로컬 경로의 규칙이다.

## 8. 전체 예시 — stdlib만 (Python)

아래 파일이 **시험 대상**이다. 통합 묶음이 이것을 그대로 띄우고 Terra의 실제 활성화
코드로 붙는다(`TestTheDocumentedHTTPContractIsEnoughForANonGoModule`). 계약이 움직이면
문서가 아니라 그 시험이 먼저 빨개진다.

> 소스: [`docs/contracts/examples/module-host-minimal.py`](examples/module-host-minimal.py)

```python
MODULE_ID  = os.environ["TERRA_MODULE_ID"]
INSTANCE_ID = os.environ["TERRA_INSTANCE_ID"]
NONCE       = os.environ["TERRA_BOOTSTRAP_NONCE"]
CREDENTIAL  = os.environ["TERRA_WORKLOAD_CREDENTIAL"]
ENDPOINT    = os.environ["TERRA_LOOPBACK_ENDPOINT"]

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.headers.get("X-Terra-Workload-Credential") != CREDENTIAL:
            self._send(401, {"error": "unauthorized"}); return
        if self.path == "/terra/handshake":
            self._send(200, {"instanceId": INSTANCE_ID, "nonce": NONCE}); return
        if self.path == "/terra/readiness":
            self._send(200, {"ready": True}); return
        if self.path == "/api/modules/" + MODULE_ID + "/v1/status":
            self._send(200, {"status": "ok", "version": "0.1.0"}); return
        self._send(404, {"error": "not found"})

host, port = ENDPOINT.rsplit(":", 1)
HTTPServer((host, int(port)), Handler).serve_forever()
```

패키지 쪽은 Go 모듈과 똑같다 — `module.json`의 `entrypoints.process`가 이 스크립트를
실행할 것을 가리키고, `contracts/api/terra-api.json`이 Operation을 선언한다.
뼈대는 `terra module new`가 굽는다(`src/`만 자기 언어로 바꾸면 된다).

## 9. 자주 틀리는 자리

| 증상 | 원인 |
| --- | --- |
| `activating`에서 멈춘다 | endpoint에 묶지 않았거나, 자격 검사에서 handshake까지 401로 막았다 |
| `identity handshake mismatch` | `instanceId`·`nonce`를 **되돌려주지 않고** 새로 만들었다 |
| 시작 즉시 재시작 반복 | `TERRA_ACTIVATION_EPOCH` 파싱 실패로 죽는다. 값은 문자열로 온 정수다 |
| route는 보이는데 404 | 계약의 `bindings` 경로와 코드의 경로가 다르다 |
| 두 번째 실행부터 401 | 자격·포트를 캐시했다. 매 실행마다 새로 발급된다 |
| 매니페스트에 적은 값이 사라진다 | v2 스키마에 없는 키다. `pack`이 이름과 함께 거절한다 |
| 같은 사람의 데이터가 앱마다 갈린다 | `X-Terra-Principal`로 나눴다. `X-Terra-Subject`로 나눈다(§7) |
| 큰 저장이 413으로 돌아온다 | 요청 본문이 1 MiB를 넘었다. 나눠 보낸다(§7.1) |

## 10. 이 계약이 사는 곳

| 무엇 | 소스 |
| --- | --- |
| 환경변수 이름 | `terra-module-runtime/identity.go` (`Env*` 상수) |
| 경로·헤더·응답 DTO | `terra-module-runtime/handshake.go` |
| 활성화 판정 | 같은 파일의 `Activate` · `NextActivationState` |
| Go 구현 | `terra-module-sdk/sdk.go` |
| 호출자 헤더 이름 | `terra-module-runtime/caller_headers.go` |
| 넘어가는 헤더 목록 | Gateway `terra-gateway-service/passthrough.go` |

문서와 코드가 어긋나면 코드가 맞다. 그리고 §8의 예시가 그 사이에 서 있다.
