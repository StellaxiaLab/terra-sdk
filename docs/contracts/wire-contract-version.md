---
title: "와이어 계약 버전 표지와 compatibility"
doc_type: "contract"
scope: "project"
target: "terra-sdk"
status: "draft"
last_updated: "2026-10-11"
related:
  - "[[docs/contracts/module-host-http-contract]]"
  - "[[docs/policy/versioning]]"
  - "[[docs/design/module-independent-repos]]"
  - "[[docs/guide/conformance]]"
---

# 와이어 계약 버전 표지와 `compatibility`

> 이 문서는 계약 문서 사본([[docs/contracts/module-host-http-contract]])의 본문을 바꾸지 않고 **덧붙이는** 문서다.
> **[제안]** 표시는 아직 확정이 아니다.

## 1. 계약 버전 (확정된 부분: 상수와 판정)

- `compat.ContractVersion`(정수, 현재 **1**)은 이 SDK가 구현한 환경변수 6 · 헤더 1 · 경로 2(+선택 경로)와 그 JSON 모양의 버전이다.
- **증가 규칙**: 호스트가 알아야 하는 추가·변경이 있을 때만 올린다. 내리지 않는다. 기존 항목의 변경·삭제는 [[docs/policy/versioning]] §3에 따라 금지(추가만)이므로, 올리는 일은 "새 항목이 생겼다"는 뜻이다.
- **표지가 없으면 v1**이다. 지금 배포된 모든 모듈과 호스트가 v1이다.
- 적합성 키트는 JSON 보고에 `wireContract`로 자기가 검사한 버전을 싣는다(`conformance.WireContractVersion`, 시험이 `compat.ContractVersion`과 같은지 고정한다).

## 2. 모듈이 요구하는 계약 버전 (manifest `compatibility`)

```json
{ "compatibility": { "core": ">=0.5.0 <1.0.0", "wireContract": 1 } }
```

| 필드 | 뜻 | 없으면 |
| --- | --- | --- |
| `core` | 이 모듈이 동작하는 Terra 코어 버전의 semver 범위 | 제한 없음 |
| `wireContract` | 모듈이 필요로 하는 **최소** 계약 버전(정수) | 1 |

파서·판정은 `compat` 패키지다(stdlib만).

```go
c, err := compat.Parse(rawCompatibilityJSON) // 문법 오류면 err
err = c.Check("0.5.9", compat.ContractVersion) // nil이면 이 호스트에서 실행 가능
```

- 범위 문법: 비교자(`>= > <= < =`)를 공백으로 이으면 AND, `||`로 이으면 OR. `^`·`~`·`x`·하이픈 범위는 **지원하지 않는다**(오류).
- **prerelease 규칙**: `0.6.0-rc.1` 같은 prerelease 버전은, 같은 집합 안의 비교자가 같은 `MAJOR.MINOR.PATCH`의 prerelease를 직접 적었을 때만 범위에 든다. 숫자상 구간 안이어도 그렇지 않으면 들지 않는다(npm과 같은 방식). 빌드 메타데이터(`+…`)는 무시한다. 우선순위는 semver 2.0.0 §11.
- 호스트의 판정이 `Check`와 같은 결과를 내야 한다. 호스트(Terra)가 이 패키지를 쓸지 자기 구현을 둘지는 Terra 쪽 결정이다. 어느 쪽이든 위 규칙이 기준이다.
- **스키마 파일의 필드 추가는 이 문서의 범위가 아니다.** manifest 스키마는 Terra가 소유하며 필드 추가는 합의 후에 한다.

### 같은 이름의 다른 개념: `pkgformat.Compatibility`

SDK에는 `Compatibility`가 둘 있고 **내용이 다르다.**

| | `compat.Compatibility` | `pkgformat.Compatibility` |
| --- | --- | --- |
| 필드 | `core`(Terra 코어 semver 범위), `wireContract`(최소 계약 버전 정수) | `products`, `hostRoles`, `keeper`, `os` |
| 뜻 | 이 모듈이 어떤 코어 버전과 와이어 계약에서 동작하는가 | 어떤 제품·호스트 역할·keeper·운영체제를 겨냥하는가 |
| 소비자 | 설치·실행 시점의 호환 판정(`Check`). 마켓 서버는 `core`·`wireContract`의 형식만 검사한다([[docs/contracts/market-http-api]]) | 패키지 매니페스트 타입(`pkgformat.Manifest`) 해석 |

둘은 매니페스트의 `compatibility` 한 블록에서 서로 다른 필드를 읽는 것이며, 한 타입으로 합치지 않았다. 이 PR은 `pkgformat`을 바꾸지 않는다.

## 3. 핸드셰이크·환경변수에 표지를 싣는 설계 **[제안]**

구현하지 않았다. 지금은 와이어 계약에 어떤 표지도 넣지 않았다.

| 방향 | 표지 | 규칙 |
| --- | --- | --- |
| 호스트 → 모듈 | 환경변수 `TERRA_CONTRACT_VERSION` (선택) | 없으면 모듈은 v1로 본다. 변수 6개라는 서술은 "필수 6개"로 남는다 |
| 모듈 → 호스트 | 핸드셰이크 응답의 `contractVersion` (선택, 정수) | 없으면 호스트는 v1로 본다. 호스트는 모르는 필드를 무시하므로 옛 호스트도 안전하다 |

판정은 manifest `compatibility.wireContract`가 1차(설치 시점)이고, 핸드셰이크의 값은 2차 확인(실행 시점)이다. 둘이 다르면 호스트 거부를 권한다. 두 표지가 모두 선택이라 이미 배포된 모듈과 호스트는 다시 빌드할 필요가 없다.
