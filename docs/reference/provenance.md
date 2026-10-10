---
title: "출처 대응표 — 원본 파일과 이 레포의 위치"
doc_type: "reference"
scope: "project"
target: "terra-sdk"
status: "draft"
last_updated: "2026-10-09"
source_repo: "StellaxiaLab/Terra"
source_commit: "b2a76a6d3f6c2d4e8a21fddc05649b95d0be6f58"
related:
  - "[[docs/design/module-independent-repos]]"
  - "[[docs/reference/public-symbols]]"
---

# 출처 대응표

이 레포의 코드는 **이동**이다: 원본을 복사하고 패키지 경로와 import만 바꿨다. 동작·시그니처·상수 값은 바꾸지 않았다.
원본은 비공개 레포 `StellaxiaLab/Terra`의 `origin/main` **`b2a76a6`** (2026-10-09 23:26 +0900)이다.
원본 경로의 앞부분 `products/common/packages/`는 아래 표에서 `P/`로 줄인다.

## 패키지와 import 경로

| 원본 Go 모듈 | 이 레포 import 경로 | 패키지명 |
| --- | --- | --- |
| `github.com/terra-project/terra/products/common/packages/terra-module-sdk` | `github.com/StellaxiaLab/terra-sdk/modulesdk` | `modulesdk` |
| `.../terra-module-runtime` (일부) | `github.com/StellaxiaLab/terra-sdk/modulert` | `modulert` |
| `.../terra-svi` (일부) | `github.com/StellaxiaLab/terra-sdk/svi` | `svi` |
| `.../terra-protocol` (일부) | `github.com/StellaxiaLab/terra-sdk/protocol` | `protocol` |

패키지명은 원본 그대로다. 호출부는 import 경로만 바꾸면 되고(T-1 별칭 전환의 근거), `modulesdk`가 쓰던 별칭 `modulert`·`coresvi`도 그대로 쓸 수 있다.

## 파일 대응

| 작업 | 원본 (`P/` 기준) | 이 레포 | 가져온 범위 |
| --- | --- | --- | --- |
| S-2 | `terra-module-sdk/{sdk,config,core_client,data_dir,svi_sink}.go` + 테스트 4개(`config_test`·`core_client_test`·`sdk_test`·`svi_resources_test`·`svi_sink_test`) | `modulesdk/` | 전체. import 3줄 치환만 |
| S-3 | `terra-module-runtime/identity.go` (+`_test`) | `modulert/identity.go` | 전체 |
| S-3 | `terra-module-runtime/handshake.go` (+`_test`) | `modulert/handshake.go` | 전체 (`Activate`·`NextActivationState` 포함) |
| S-3 | `terra-module-runtime/hostadapter.go` (+`_test` 일부) | `modulert/hostadapter.go` | `CoreInvocation`·`CoreResult`만. `HostAdapter` 인터페이스·카탈로그 타입은 호스트 쪽이라 제외 |
| S-3 | `terra-module-runtime/svi_sink.go` | `modulert/svi_sink.go` | `SVISinkPath`·`SVISinkOp*`·`SVISinkRequest/Response`. `Manifest`에 의존하는 `ManifestProvidesSVISink`·`contributionsSVISinkKey` 제외 |
| S-3 | `terra-module-runtime/capability_broker.go` | `modulert/capability_broker.go` | `ErrCoreOperationDenied`만. 브로커는 호스트 쪽 |
| S-3 | `terra-module-runtime/state_store.go` | `modulert/state_store.go` | `DataDirName`·`ModuleDataDir`만 |
| S-3 | `terra-module-runtime/config_store.go` | `modulert/config_store.go` | `ConfigFileEnv`만 |
| S-3 (연쇄) | `terra-module-runtime/lifecycle.go` (+`_test`) | `modulert/lifecycle.go` | 전체 — `handshake.go`가 `State`를 쓴다 |
| S-3 (연쇄) | `terra-module-runtime/manifest.go` | `modulert/readiness.go` | `Readiness` 구조체만 — `Activate`가 받는다 |
| S-4 | `terra-module-runtime/gateway_delegate.go` | `modulert/gateway_delegate.go` | 전체 |
| S-4 | `terra-module-runtime/shared_roots.go` | `modulert/shared_roots.go` | `DeclaresSharedRoots`·`declaresStorage`(Manifest 의존) 제외 |
| S-5 | `terra-svi/types.go` | `svi/types.go` | 전체 |
| 후속(io-inventory) | `terra-svi/validation.go` | `svi/validate.go` | `ValidateResource`와 호출 연쇄(`ValidateEndpoint` · 값 검증 함수 · `validateUniqueStrings` · `blank`)와 오류 3개(`ErrInvalidResource` · `ErrInvalidEndpoint` · `ErrInvalidSchemaRef`) · `kindPattern`. 함수 본문은 원본과 동일. `ValidateHandleRequest` · `ValidateBinding` · `ValidateRuntimeStatus`는 호스트 쪽이라 제외 |
| 후속(io-inventory) | `terra-svi/schema_ref.go` (+`_test`) | `svi/schema_ref.go` | 전체 |
| 후속(io-inventory) | `terra-svi/validation_test.go`의 서술자 검증 4개, `terra-svi/fixtures/{valid,invalid}/…` 3개 | `svi/validate_test.go`, `svi/testdata/` | 시험 4개와 고정 입력 3개. 고정 입력 경로만 `fixtures` → `testdata` |
| 후속(io-weave) | `terra-testwait/testwait.go` (+`_test`) | `testwait/` | 코드 본문 동일, 패키지 설명 주석만 이 레포용으로 다시 씀. 저장소 전체를 훑는 `guard_test.go`는 Terra 전용이라 제외 |
| S-6 | `terra-protocol/{inputevent,inputpermission,virtualinput}.go` (+`_test` 3개) | `protocol/` | 전체 |
| S-7 | `tests/module-gateway-integration/module_contract_doc_test.go` | `conformance/` | 계약 검증 부분을 새로 작성(복사 아님). 아래 참조 |
| S-8 | `docs/contracts/module-host-http-contract.md` | `docs/contracts/` | 본문 무변경, 머리에 사본 안내만 추가 |
| S-8 | `docs/contracts/examples/module-host-minimal.py` | `docs/contracts/examples/` | 바이트 동일 |

## 원본에서 바꾼 것 (전부)

- import 경로 치환 (`modulesdk`의 소스와 테스트)
- 부분 가져온 파일의 머리에 "Subset of …" 주석 추가, 제외한 선언 삭제
- 패키지 설명용 `doc.go` 추가 (`modulert` · `svi` · `protocol`)
- 파일 안의 주석은 손대지 않았다. 그래서 Terra 내부 설계 절 번호(§12.1 등)와 한국어 주석이 남아 있다.

## 후속 추가 (v0.2.0)

`io-inventory`의 시험이 `svi.ValidateResource`를, `io-weave`의 시험이 `testwait`의 `Budget`을 부른다. 둘 다 사용자 결정(2026-10-10)으로 SDK에 공개했다.
설계 문서 §3은 `terra-svi`의 "검증"을 공개하지 않는 것으로 적었다. 이 추가는 그 문장의 일부(서술자 검증)를 바꾼 결정이다.

**Terra에 같은 코드가 둘이 된다.** Terra의 `terra-svi`는 타입을 SDK 별칭으로 바꿨지만(T-1) `ValidateResource`는 자기 사본을 갖고 있다. 오류 변수도 별개라
`errors.Is(err, sdksvi.ErrInvalidResource)`는 Terra의 오류에 거짓이다. 설계 D-3("원본은 공개 SDK 한 곳")대로 하려면 Terra 쪽이 SDK 것을 부르게 바꿔야 한다(Terra 후속 작업).

## S-7은 복사가 아니다

원본 시험은 Terra의 `modulert.Activate`로 예제를 붙이고 `repoRoot`(Terra 체크아웃 위치)에 기댄다. 그대로 옮기면 Terra가 필요하므로,
계약(환경변수 6 · 헤더 1 · 경로 2)만 보는 형태로 새로 썼다. 자세한 것은 [[docs/guide/conformance|적합성 테스트 키트]]를 본다.
원본 묶음의 나머지(실제 Gateway·daemon·scaffold 연동 시험)는 가져오지 않았다.

## 읽은 방법

로컬 Terra 작업 트리는 읽지 않았다. 원본은 `origin/main`의 얕은 sparse 체크아웃(`b2a76a6`)에서만 읽었다.
