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
선택 경로(`/terra/svi/*`, `/terra/core/invoke`)도 아직 검사하지 않는다.

## 명령줄 (Go가 아닌 모듈)

`@latest` 형태는 `v0.1.0` 태그 이후에 동작한다. 그 전에는 이 레포 안에서 `go run ./cmd/terra-conformance`로 돌린다.

```sh
go run github.com/StellaxiaLab/terra-sdk/cmd/terra-conformance@latest \
  [-module-id io.example.mine] [-operation /api/modules/io.example.mine/v1/status] \
  [-bind-timeout 20s] [-launches 2] \
  -- python3 my_module.py
```

종료 코드: `0` 모두 통과 · `1` 규칙 위반 · `2` 사용법/하네스 오류(명령을 못 띄움 등).
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

## Terra 원본 시험과의 차이

원본 `TestTheDocumentedHTTPContractIsEnoughForANonGoModule`(Terra `products/common/tests/module-gateway-integration`)는
Terra의 활성화 코드(`IdentityIssuer.Issue` → `Activate` → `NextActivationState`)로 예제를 붙인다. 키트는 같은 요청을 직접 보낸다.
원본 묶음의 나머지 시험(실제 Gateway·daemon·scaffold 연동)은 Terra 코드가 있어야 하므로 이 레포로 가져오지 않았다.
