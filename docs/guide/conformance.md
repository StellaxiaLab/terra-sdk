---
title: "적합성 테스트 키트"
doc_type: "guide"
scope: "project"
target: "terra-sdk"
status: "draft"
last_updated: "2026-10-09"
related:
  - "[[docs/contracts/module-host-http-contract]]"
  - "[[docs/contracts/examples/README]]"
  - "[[docs/policy/versioning]]"
---

# 적합성 테스트 키트

모듈이 [[docs/contracts/module-host-http-contract|모듈 측 HTTP 계약]]을 지키는지, **Terra 없이** 검사한다.
키트가 호스트 역할을 한다: 새 신원을 발급해 모듈을 띄우고, 호스트가 하는 요청을 보내고, 규칙마다 PASS/FAIL을 낸다.

Terra 코드를 import하지 않는다. 계약(환경변수 6 · 헤더 1 · 경로 2)을 리터럴로 들고 raw HTTP만 쓰므로
**어떤 언어로 만든 모듈이든** 검사할 수 있다.

## 검사하는 규칙

| 규칙 | 계약 |
| --- | --- |
| `GET /terra/handshake`가 발급받은 `instanceId`·`nonce`를 그대로 되돌려준다 | §4 |
| `GET /terra/readiness`가 `{"ready": true}`로 답한다 | §5 |
| 자격 헤더(`X-Terra-Workload-Credential`)가 없거나 틀리면 두 제어 경로가 **401**이고, 본문에 발급된 값을 싣지 않는다 | §3 |
| (선택) 모듈 자신의 경로도 같은 자격을 요구하고, 자격이 있으면 200이다 | §3, §6 |
| 모듈을 두 번 띄워도(매번 새 신원) 위 규칙이 모두 통과한다 — 신원·포트를 캐시하지 않는다 | §2 |

호스트가 모듈 쪽에서 구분할 수 없는 부분(예: `identity handshake mismatch` 판정 자체)은 검사하지 않는다.

## 선택 경로 (`-svi`, `-core-invoke`)

계약 §6이 근거를 주는 규칙만 켠다. 기본은 꺼져 있다.

| 플래그 | 규칙 | 결과 |
| --- | --- | --- |
| `-svi` | `/terra/svi/resources`·`/terra/svi/sink`가 자격 없이/틀린 자격으로 401 | pass/fail |
| `-svi` | `/terra/svi/resources`가 200과 유효한 자원 서술자 배열(`svi.ValidateResource`)로 답한다 | pass/fail |
| `-svi` | sink의 `open`/`write`/`close` 본문 | **skip** - 계약이 본문 모양을 검사할 만큼 정하지 않았다 |
| `-core-invoke` | `/terra/core/invoke` | **skip** - 모듈에서 호스트로 가는 방향이라 호스트가 서빙한다. 키트는 브로커를 흉내 내지 않는다 |

## 결과 수준과 JSON

수준은 `pass` · `fail` · `warn` · `skip` 넷이다. **fail만 보고를 실패로 만든다.** `warn`은 단계적으로 도입 중인 규칙([[docs/policy/versioning]] §5)에, `skip`은 검사하지 않은 규칙(사유 포함)에 쓴다. 현재 키트가 내는 `warn`은 없다(수준만 준비됨).

`-json`은 stdout에 JSON 문서 **하나만** 낸다(사람이 읽는 줄은 내지 않는다). 하네스 오류(종료 2)에도 JSON을 내며 `error`가 채워진다.

```json
{
  "schemaVersion": 1,
  "ok": true,
  "wireContract": 1,
  "results": [
    {"name": "[launch 1] handshake echoes instanceId and nonce", "status": "pass"},
    {"name": "[launch 1] core invoke: /terra/core/invoke", "status": "skip", "error": "module-to-host direction: ..."}
  ]
}
```

`ok`는 `fail`이 없고 `pass`가 하나 이상일 때만 true이며, 하네스 오류가 있으면 false다. `error`는 하네스 오류일 때만, `results[].error`는 `fail`·`skip` 사유로 채워진다. `schemaVersion` 안에서는 필드를 더하기만 한다. `rule`(안정적인 규칙 id)은 선택 필드로 아직 채우지 않는다.
Go에서는 `Report.ToJSON(harnessErr)`와 `Report.Summary()`를 쓴다. 다른 도구에서 실행 파일을 직접 검사하려면 `conformance.Run(ctx, Spec{Command, Args, Dir, ...})`로 실행 파일을 직접 검사할 수 있다. 모듈 디렉터리를 풀어 manifest의 진입점으로 띄우는 일은 패키지 형식을 소유한 쪽의 일이라 키트에 없다.

## 명령줄 (Go가 아닌 모듈)

`@latest` 형태는 `v0.1.0` 태그 이후에 동작한다. 그 전에는 이 레포 안에서 `go run ./cmd/terra-conformance`로 돌린다.

```sh
go run github.com/StellaxiaLab/terra-sdk/cmd/terra-conformance@latest \
  [-module-id io.example.mine] [-operation /api/modules/io.example.mine/v1/status] \
  [-bind-timeout 20s] [-launches 2] [-json] [-svi] [-core-invoke] \
  -- python3 my_module.py
```

종료 코드: `0` 모두 통과(warn·skip은 실패 아님) · `1` 규칙 위반 · `2` 사용법/하네스 오류(명령을 못 띄움 등). 마켓 서버는 키트를 실행하지 않는다. 퍼블리셔가 이 결과를 요약해 자가 보고로 붙일 뿐이며, 자가 보고는 업로드를 막지 않는다([[docs/guide/publisher-guide]], [[docs/contracts/market-http-api]] §4.5).
모듈은 자식 프로세스로 뜨며 환경에 6개 변수가 주입된다. 키트는 자기가 띄운 프로세스만 종료한다.

## Go 테스트에서

```go
import (
    "testing"

    "github.com/StellaxiaLab/terra-sdk/conformance"
    "github.com/StellaxiaLab/terra-sdk/conformance/conformancetest"
)

func TestMyModuleHonoursTheContract(t *testing.T) {
    conformancetest.Run(t, conformance.Spec{
        Command:       "python3",
        Args:          []string{"my_module.py"},
        ModuleID:      "io.example.mine",
        OperationPath: "/api/modules/io.example.mine/v1/status",
    })
}
```

이미 떠 있는 endpoint는 `conformance.NewIdentity` + `conformance.Check`로 직접 검사할 수 있다(프로세스를 띄우지 않는다).

## 이 레포의 자체 시험

| 시험 | 뜻 |
| --- | --- |
| 문서의 Python 예제가 통과한다 | "표준 라이브러리만으로 충분하다"는 계약의 주장을 실행으로 확인 |
| 이 SDK(`modulesdk`)로 만든 Go 모듈이 통과한다 | SDK가 계약을 지킨다 |
| 위반 5종(nonce 재생성 · 미준비 · 자격 미검사 · 401 본문 누설 · 403 응답)이 각각 FAIL로 잡힌다 | 키트가 실제로 위반을 본다 |
| 키트의 계약 리터럴이 `modulert` 상수와 같다 | 한쪽만 바뀌면 여기서 먼저 빨개진다 |
| SVI 규칙 위반 4종(자격 미검사 2 · 배열 아님 · 잘못된 서술자)이 FAIL로 잡히고, 플래그가 없으면 SVI 규칙이 돌지 않는다 | 선택 경로 검사 |
| JSON 보고의 모양, 하네스 오류 시 `-json` 출력 | 퍼블리셔 도구가 읽는 형식 고정 |

## Terra 원본 시험과의 차이

원본 `TestTheDocumentedHTTPContractIsEnoughForANonGoModule`(Terra `products/common/tests/module-gateway-integration`)는
Terra의 활성화 코드(`IdentityIssuer.Issue` → `Activate` → `NextActivationState`)로 예제를 붙인다. 키트는 같은 요청을 직접 보낸다.
원본 묶음의 나머지 시험(실제 Gateway·daemon·scaffold 연동)은 Terra 코드가 있어야 하므로 이 레포로 가져오지 않았다.
