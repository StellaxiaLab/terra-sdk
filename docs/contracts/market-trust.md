---
title: "마켓 신뢰 형식 계약"
doc_type: "contract"
scope: "project"
target: "terra-sdk"
status: "draft"
version: "0.3-draft"
last_updated: "2026-10-12"
related:
  - "[[docs/policy/versioning]]"
  - "[[docs/reference/public-symbols]]"
  - "[[docs/contracts/market-http-api]]"
---

# 마켓 신뢰 형식 계약

패키지 마켓과 Terra 노드가 **같은 바이트**에 서명하고 같은 판정을 내리도록 `trust` 패키지가 정한 형식이다.
서명은 바이트를 덮으므로, 이 문서의 정규화 규칙이 곧 계약이다. 이 문서는 **서명·인증서·폐기 형식**의 정본이고, 그 객체가 HTTP로 오가는 경로·요청·응답·오류 코드는 [[docs/contracts/market-http-api|마켓 HTTP 프로토콜 계약]]이 정본이다. 한 사실은 한쪽에만 적는다. 코드는 [`trust`](../../trust)와 [`pkgformat`](../../pkgformat), 골든 벡터는 [`trust/testdata/market_vectors.json`](../../trust/testdata/market_vectors.json)이다.

> 안정성: 두 패키지는 v0.x 불안정 등급이지만 **서명 바이트 형식은 §8의 더 엄격한 규칙**을 따른다.

## 1. 서명 알고리즘과 키 식별

- 알고리즘은 `ed25519`뿐이다. 값(`value`)은 서명 바이트의 **base64url, 패딩 없음(raw)** 이다.
- 키 ID(`keyId`) = `base64url_raw( sha256(공개키 32바이트)[:12] )`. 공개키 문자열(`publicKey`)도 base64url raw다.
- 서명하는 쪽의 `issuer`는 접두사로 종류를 가른다: `market:<marketId>`, `tree:<id>`, `publisher:<id>`.

## 2. 서명 payload 정규화 (바이트 단위 계약)

모든 JSON 정규화는 Go `encoding/json`의 동작으로 정의된다. 다른 언어의 구현은 이를 재현해야 한다.

| payload | 규칙 |
| --- | --- |
| 작성자 서명 (`CanonicalManifestPayload`) | 매니페스트 원문을 `map[string]any`로 읽고(`json.Unmarshal`), 최상위 `signature` 키를 지운 뒤 `json.Marshal`한다. 키는 사전순으로 정렬되고, 숫자는 `float64`로 읽었다가 쓰이며(`12345678901234567890` → `12345678901234567000`, `1e21` → `1e+21`), 문자열은 `<` `>` `&` 와 U+2028/U+2029가 `<` `>` `&` ` ` ` `로 이스케이프되고, 잘못된 UTF-8은 U+FFFD로 치환된다. 공백·들여쓰기는 결과에 영향이 없다 |
| 카운터서명 (`CanonicalCounterSignPayload`) | 위와 같되 `signature` 블록은 **남기고** 그 안의 `chain` 키만 지운다. 따라서 카운터서명은 작성자 서명(`signature.value` 등)을 덮고, 체인에 항목이 더해져도 payload가 바뀌지 않는다 |
| 마켓 인증서 (`CanonicalMarketCertPayload`) | 필드 **선언 순서** 그대로 `kind`(`"terra.market.keycert"`), `marketId`, `role`, `keyId`, `publicKey`, `epoch`, `notBefore`, `notAfter`를 `json.Marshal`한다. `signatures`는 포함하지 않는다 |
| 폐기 스냅숏 (`CanonicalSnapshotPayload`) | 선언 순서 `kind`(`"terra.market.revocations"`), `marketId`, `sequence`, `issuedAt`, `expiresAt`, `revoked`, `latest`. `revoked`는 (id, version, digest) 순으로 안정 정렬한 사본이며 비어 있으면 `[]`, `latest`가 없으면 `{}`, 항목의 `digest`는 비면 생략(omitempty). `cert`·`signature`는 포함하지 않는다 |

시험이 이 규칙을 지킨다: `trust/payload_golden_test.go`(정규화 바이트), `TestMarketVectors`(벡터), `TestVectorFileIsPinned`(벡터 파일의 sha256 고정).

## 3. 매니페스트의 서명 블록과 카운터서명

```json
"signature": { "algorithm": "ed25519", "keyId": "...", "issuer": "...", "value": "..." }
```

작성자 서명은 §2 첫 줄의 payload에 대한 서명이다. 카운터서명(`CounterSignature`)은 패키지 바깥(게시 기록)에 놓이는 체인 항목이다:

| 필드 | 뜻 |
| --- | --- |
| `algorithm` | `ed25519` |
| `keyId`, `issuer`, `signedAt`(선택) | 서명 키, 서명자, 서명 시각 |
| `value` | §2 카운터서명 payload에 대한 서명 |
| `cert`(선택) | 마켓 증명 항목일 때만: 루트가 서명한 증명 키 인증서 |

## 4. 마켓 루트와 키 인증서

- **루트**(`MarketRoot`)는 제품에 내장된 오프라인 공개키 집합과 임계값이다(`marketId`, `threshold`, `keys`). 임계값은 서로 **다른** 루트 키의 서명 개수다. 임계값이 0이거나 키가 임계값보다 적으면 루트는 꺼진 것(`Enabled() == false`)이다.
- **키 인증서**(`MarketKeyCertificate`)는 온라인 키(`role`: `attest` 또는 `revoke`)에 대한 루트의 서명 묶음이다. 필드: `role`, `keyId`, `publicKey`, `epoch`, `notBefore`, `notAfter`(RFC 3339), `signatures[]`.
- 검증(`VerifyMarketCertificate`) 순서: ① 루트가 켜져 있다 → ② `role`이 기대값(`attest`|`revoke`)이다 → ③ `publicKey`가 ed25519 키 길이이고 `keyId`가 그 키에서 유도한 값과 같다 → ④ `notBefore < notAfter`이고 현재 시각이 `[notBefore, notAfter)` 안이다 → ⑤ `signatures` 중 루트 키로 검증되는 **서로 다른 키**의 개수가 임계값 이상이다. 같은 키의 중복 서명은 한 번만 센다.

## 5. 체인 검증

`VerifyChain(raw, chain, anchors, now)`는 각 항목을 독립적으로 본다. 키를 모르거나 인증서가 만료·불일치이거나 서명이 틀린 항목은 **오류 없이 건너뛴다**(증명을 하나도 못 얻을 뿐).

- `market:` 항목: `anchors.Market`이 켜져 있고, 항목에 `cert`가 있으며, `issuer`가 루트의 `market:<marketId>`와 같고, 인증서가 `attest` 역할로 검증되고, 인증서 키와 `keyId`가 같고, §2 카운터서명 payload 서명이 맞아야 한다.
- 그 밖의 항목: `anchors.Tree`에서 `keyId`가 같은 앵커의 키로 서명을 검증한다.
- 결과는 검증된 **모든** 증명을 기록한다(어느 쪽이 더 높은지 고르지 않는다). 등급은 마켓 증명이 하나라도 있으면 `trusted-third-party`, 없고 트리 앵커가 있으면 그 앵커의 등급(기본 `tree-published`)이다.

## 6. 폐기 스냅숏

`RevocationSnapshot`: `marketId`, `sequence`(단조 증가), `issuedAt`, `expiresAt`, `revoked[]`(`id`, `version`(`*` 가능), `digest`(선택), `reason`: `security`|`policy`|`publisher`), `latest`(id → 최신 버전), `cert`(`revoke` 인증서), `signature`.

- `VerifySnapshot`: `marketId`가 루트와 같다 → `cert`가 `revoke` 역할로 §4 검증을 통과한다 → `signature.keyId`가 인증서 키와 같다 → §2 payload 서명이 맞다. **만료는 오류가 아니다**(`Fresh`로 따로 본다).
- `AcceptSnapshot(root, prev, next, now)`: 검증 후 `sequence`가 `prev`보다 낮으면 `ErrSnapshotRollback`, 같으면 `ErrSnapshotSame`, `next`가 이미 만료면 `ErrSnapshotExpiredNew`.
- `Judge(id, version, digest)`: `id`가 같고 `version`이 `*`이거나 같고, 항목에 `digest`가 있으면 정규화(소문자, `sha256:` 접두 제거) 후 같은 항목이 해당한다. 여럿이 해당하면 `security`가 이긴다.
- `CheckDowngrade`: 고정(pin)되지 않았고 설치된 버전이 있을 때 후보가 더 낮으면 `ErrDowngrade`.
- `MarketGate.Check`는 위 판정을 한 번에 묶은 설치 게이트다: 스냅숏 없음/만료 → `ErrRevocationStale`(`AllowStale`이면 통과), 폐기 → `ErrRevoked`, 다운그레이드 → `ErrDowngrade`.

## 7. 패키지 형식 (`pkgformat`)

마켓이 업로드를 노드와 같은 규칙으로 판정하도록 `pkgformat`이 가진 것: `Manifest`(v2)와 `Validate`, v1 매니페스트와 v2로의 정규화(`DecodeManifest`는 v1·v2를 모두 받는다), 모듈 ID 규칙(`ValidModuleID`: `^[a-z0-9][a-z0-9._-]{2,127}$`), 설정 스키마(`ParseConfigSchema`), 정규 권한 문자열(`kind:value`, 정렬·중복 제거), `.tmod` 아카이브 읽기/쓰기와 한도(`DefaultArchiveLimits`: 항목 10,000개, 총 2GiB, 매니페스트 4MiB, 압축비 200).

## 8. 변경 규칙

- §2의 정규화 규칙과 §3~§6의 JSON 필드·의미는 **하위 호환을 깨지 않는다**: 이미 서명된 패키지·인증서·스냅숏이 계속 검증되어야 한다. 새 필드는 서명 payload에 들어가지 않는 선택 필드로만 더한다.
- payload에 들어가는 필드나 순서를 바꿔야 하면 새 `kind` 문자열을 가진 새 형식을 추가하고 옛 형식은 검증 가능한 채로 둔다.
- 오류 메시지 문자열과 `Err*` 값의 동일성은 호출자가 비교할 수 있으므로 PATCH에서 바꾸지 않는다.
- 이 보장은 `trust` 패키지의 서명·검증 형식에 대한 것이다. `pkgformat`의 Go API는 v0.x MINOR에서 바뀔 수 있다([[docs/policy/versioning]] §3).
