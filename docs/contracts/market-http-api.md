---
title: "마켓 HTTP 프로토콜 계약"
doc_type: "contract"
scope: "project"
target: "terra-sdk"
status: "draft"
version: "0.1-draft"
last_updated: "2026-10-12"
related:
  - "[[docs/contracts/market-trust]]"
  - "[[docs/guide/publisher-guide]]"
  - "[[docs/policy/versioning]]"
---

# 마켓 HTTP 프로토콜 계약

패키지 마켓 서버와 그 클라이언트(노드, 퍼블리셔 도구, 미러)가 주고받는 **전송 계약**이다. 경로, 요청·응답 JSON, 오류 코드, 캐시 규칙을 정한다.

- **서명 바이트, 인증서, 루트, 폐기 스냅숏의 형식과 검증 규칙**은 [[docs/contracts/market-trust|마켓 신뢰 형식 계약]]이 정본이다. 이 문서는 그 객체가 HTTP로 *오가는 모양*만 다루며, 서명 규칙을 다시 적지 않는다.
- 형식 코드는 [`trust`](../../trust)와 [`pkgformat`](../../pkgformat)에 있다. 이 문서의 응답 객체 중 `chain[]`, `cert`, 폐기 스냅숏은 `trust`의 JSON 형식 그대로다.
- 서버 구현은 이 레포에 없다. 이 계약을 따르면 누구든 호환 서버나 클라이언트를 만들 수 있다.

> 안정성: 아직 초안(0.1-draft)이다. 경로와 필드의 의미는 하위 호환을 깨지 않는 방향으로만 바꾼다(§9).

## 1. 공통 규칙

- 기본 베이스 경로는 `/market/v1`이다. 아래 경로는 모두 이 아래에 있다.
- 본문은 JSON(UTF-8)이다. 시간은 RFC 3339 UTC. 패키지 식별자(digest)는 `sha256:<소문자 hex>`.
- 읽기 API(§3)는 **익명**이고 캐시 가능하다. 퍼블리셔 API(§4)는 `Authorization: Bearer <퍼블리셔 토큰>`이 필요하다(§4.1). 토큰은 등록 때 한 번만 보이고, 서버는 해시만 저장한다.
- 새 응답 필드는 추가될 수 있다. 클라이언트는 모르는 필드를 무시해야 한다.

### 1.1 오류 모양

모든 엔드포인트가 같은 모양으로 오류를 낸다.

```json
{ "error": { "code": "PREFIX_NOT_OWNED", "message": "…", "details": { } } }
```

`message`는 사람용이며 바뀔 수 있다. 프로그램은 `code`로 분기한다. `details`는 선택이다.

| HTTP | 뜻 |
| --- | --- |
| 400 | 입력 오류 |
| 401 | 인증 없음 또는 알 수 없는 토큰 |
| 403 | 권한 없음, 예약된 접두사 |
| 404 | 없음 |
| 409 | 충돌 |
| 410 | 만료 |
| 413 | 크기 초과 |
| 422 | 업로드 검사 거부 |
| 429 | 쿼터 초과 |
| 500 | 서버 내부 오류 |
| 503 | 일시적으로 쓸 수 없음 |

### 1.2 오류 코드

| 코드 | HTTP | 발생 |
| --- | --- | --- |
| `BAD_REQUEST` | 400 | 잘못된 JSON, 필드, 쿼리 값, 지원하지 않는 증명 방식 |
| `DIGEST_MISMATCH` | 400 | 업로드 본문의 digest가 `X-Terra-Digest`와 다름 |
| `BAD_TARGET` | 422 | `targets_extra`가 같은 id·version의 접수된 업로드가 아님 |
| `UNAUTHENTICATED` | 401 | 토큰 없음/알 수 없음 |
| `PREFIX_RESERVED` | 403, 422 | `io.terra` 및 그 아래 접두사(프로젝트 예약) |
| `PREFIX_NOT_OWNED` | 409, 422 | 접두사가 다른 퍼블리셔 소유이거나 겹침, 또는 모듈 id를 덮는 검증된 접두사가 없음 |
| `KEY_EXISTS` | 409 | 이미 등록된 키 |
| `KEY_NOT_REGISTERED` | 422 | 매니페스트를 서명한 키가 이 퍼블리셔의 활성 키가 아님 |
| `SIGNATURE_INVALID` | 422 | 키 소유 증명 또는 작성자 서명이 검증되지 않음 |
| `PERMISSIONS_MISSING` | 422 | 매니페스트에 `permissions` 블록이 없음(빈 블록은 허용) |
| `INVALID_PACKAGE` | 422 | 패키지 검사 실패, 권한 블록이 정규화되지 않음, `compatibility` 형식 오류 |
| `VERSION_EXISTS` | 409, 422 | 같은 id+version이 이미 게시됨(불변) |
| `PACKAGE_TOO_LARGE` | 413 | `limits.max_package_bytes` 초과 |
| `QUOTA_EXCEEDED` | 429 | 최근 24시간 업로드 쿼터 소진 |
| `NOT_FOUND` | 404 | 없는 모듈·버전·패키지·스냅숏·claim·게시물 |
| `CONFLICT` | 409 | 상태가 맞지 않음(예: 이미 yank된 게시물을 다시 yank) |
| `CLAIM_CLOSED` | 409 | GitHub 증거를 이미 받았거나 pending이 아닌 claim |
| `CLAIM_EXPIRED` | 410 | 만료된 claim |
| `UNAVAILABLE` | 503 | 증명 방식이 꺼져 있거나 검사를 돌릴 수 없음 |
| `INTERNAL` | 500 | 서버 내부 오류 |

## 2. 모델

- **퍼블리셔**: 토큰을 가진 계정. 서명 키(ed25519)와 검증된 접두사를 가진다.
- **접두사**: 모듈 id의 전역 접두사(예: `com.acme`). 경계는 `<접두사>.`이다 — `com.acme`는 `com.acme.tool`을 소유하지만 `com.acmex.tool`은 아니다. 접두사 하나의 소유자는 하나이고, 서로 포함 관계인 접두사를 다른 퍼블리셔가 가질 수 없다. `io.terra`와 그 아래는 예약이다.
- **패키지(업로드)**: 검사를 통과한 `.tmod`. digest로 식별한다. 업로드만으로는 공개되지 않는다.
- **게시물(publication)**: 한 모듈의 한 버전. target(`any`, `windows-amd64` 등)별로 패키지 digest를 가리킨다. 한번 게시된 id+version은 바뀌지 않는다.
- **마켓 증명(chain)**: 게시할 때 서버가 매니페스트에 붙이는 카운터서명([[docs/contracts/market-trust|신뢰 형식]] §3, §5). 매니페스트 본문과 패키지 바이트는 바뀌지 않는다.
- **폐기 스냅숏**: 서버가 서명해 발행하는 단조 증가 목록([[docs/contracts/market-trust|신뢰 형식]] §6).

## 3. 읽기 API (익명)

| 요청 | 설명 |
| --- | --- |
| `GET /market/v1/info` | 마켓 메타와 현재 인증서 |
| `GET /market/v1/modules?q=&prefix=&target=&cursor=&limit=` | 카탈로그 목록 |
| `GET /market/v1/modules/{id}` | 모듈 상세(버전 목록) |
| `GET /market/v1/modules/{id}/versions/{version}` | 한 버전: 매니페스트, 권한, targets, chain, 폐기 상태 |
| `GET /market/v1/packages/{digest}` | `.tmod` 바이트 |
| `GET /market/v1/revocations/latest` | 최신 폐기 스냅숏 |
| `GET /market/v1/revocations/{sequence}` | 지난 스냅숏 |

### 3.1 `GET /info`

```json
{ "market_id": "terra", "api_version": 1,
  "attest_certificate": { "role": "attest", "keyId": "…", "publicKey": "…", "epoch": 1, "notBefore": "…", "notAfter": "…", "signatures": [ … ] },
  "revoke_certificate": { … },
  "limits": { "max_package_bytes": 67108864 } }
```

두 인증서의 형식은 [[docs/contracts/market-trust|신뢰 형식]] §4의 `MarketKeyCertificate`다. `Cache-Control: public, max-age=300`.

### 3.2 `GET /modules`

쿼리: `q`(id·이름·설명에 대한 대소문자 무시 부분 일치), `prefix`(id가 `prefix`와 같거나 `prefix.`로 시작), `target`(그 target 또는 `any`를 가진 것), `cursor`, `limit`(기본 50, 최대 200, 1 미만은 `400`).

```json
{ "items": [ {
    "id": "com.acme.hello", "name": "Hello", "description": "…",
    "publisher": { "id": "pub_…", "name": "ACME", "prefixes": ["com.acme"] },
    "latest_version": "1.2.0", "targets": ["any"],
    "permissions": ["core.operation:terra.node.read", "network:api.example.com"],
    "updated_at": "…", "revoked": false,
    "compatibility": { "core": ">=0.5.0 <1.0.0", "wireContract": 1 },
    "conformance": { "source": "self_reported", "signed": false, "status": "pass", "kit": "terra-conformance", "target": "linux-amd64" } } ],
  "next_cursor": "" }
```

- 목록은 id 사전순이다. `next_cursor`는 다음 페이지를 시작할 `cursor`(직전 페이지의 마지막 id)이고, 더 없으면 빈 문자열이다.
- `latest_version`은 live 상태인 버전 중 가장 높은 semver다(live가 없으면 가장 높은 버전).
- `permissions`는 매니페스트 `permissions` 블록의 **정규 권한 집합**(`kind:value`, 정렬·중복 제거)의 **표시용 사본**이다. 정규 형식은 `pkgformat.CanonicalPermissionsOf`가 정한다. 클라이언트는 판정에 이 값을 쓰지 말고, 서명된 매니페스트에서 직접 계산해야 한다.
- `revoked`는 서버가 가진 최신 스냅숏으로 그 버전을 판정한 값이다.
- `compatibility`는 매니페스트에 있을 때만 실린다.
- `conformance`는 퍼블리셔가 붙인 **자가 보고**가 있을 때만 실린다(§4.5). `source`는 항상 `"self_reported"`, `signed`는 항상 `false`다 — 서버가 확인한 것이 아니다.
- `Cache-Control: public, max-age=30`.

### 3.3 `GET /modules/{id}`

§3.2의 항목 형식에 `versions`가 더해진다.

```json
{ "id": "…", …, "versions": [ { "version": "1.2.0", "state": "live", "published_at": "…", "digest": "sha256:…" } ] }
```

`versions`는 높은 버전부터이며, `state`는 `live` 또는 `yanked`다. `digest`는 `any` target의 digest다. 없는 id는 `404 NOT_FOUND`.

### 3.4 `GET /modules/{id}/versions/{version}`

설치와 미러의 소비 단위다.

```json
{ "id": "com.acme.hello", "version": "1.2.0", "state": "live", "published_at": "…",
  "targets": { "any": "sha256:…", "windows-amd64": "sha256:…" },
  "manifest": { … 서명 블록을 포함한 매니페스트 원문 객체 … },
  "permissions": ["…"],
  "chain": [ { "algorithm": "ed25519", "keyId": "…", "issuer": "market:terra", "signedAt": "…", "value": "…", "cert": { … } } ],
  "revocation": { "revoked": false },
  "conformance": { "source": "self_reported", "signed": false, "status": "pass", "kit": "…", "target": "…", "detail": "…" } }
```

- `manifest`는 패키지 안의 매니페스트와 **바이트 단위로 같은** JSON 원문이다. 클라이언트는 `chain`의 마켓 증명을 마켓 루트로 검증한 뒤에야 `manifest`를 믿는다([[docs/contracts/market-trust|신뢰 형식]] §5). 권한은 이 매니페스트에서 계산한다(`permissions` 필드는 표시용).
- `chain`에는 **마켓 증명만** 있다(미러가 덧붙이는 부서명은 미러가 자기 쪽에서 더한다).
- `targets`는 target 이름에서 패키지 digest로의 맵이다. 증명은 매니페스트 기준이라 target과 무관하다. target 이름은 `any` 또는 `<goos>-<goarch>`다.
- `revocation`은 `{ "revoked": false }` 또는 `{ "revoked": true, "reason": "security|policy|publisher" }`다. 클라이언트는 이 값 대신 서명된 스냅숏(§3.6)으로 판정해야 한다.
- 없는 버전은 `404 NOT_FOUND`. `Cache-Control: public, max-age=30`.

### 3.5 `GET /packages/{digest}`

`.tmod`(zip) 바이트를 돌려준다.

- `Content-Type: application/zip`, `ETag: "<digest>"`, `X-Terra-Digest: <digest>`, `Cache-Control: public, max-age=31536000, immutable`. 바이트 범위 요청(`Range`)을 지원한다.
- **어떤 게시물의 `targets`가 가리키는 digest만** 내려준다. 접수만 되고 게시되지 않은 업로드는 `404`다.
- yank된 패키지도 계속 내려받을 수 있다. 이미 설치한 노드가 다시 받을 수 있어야 하기 때문이다. "설치하지 말 것"은 404가 아니라 폐기 스냅숏이 알린다.
- 클라이언트는 받은 바이트의 sha256이 요청한 digest와 같은지 반드시 확인한다.

### 3.6 폐기 스냅숏

`GET /revocations/latest`와 `GET /revocations/{sequence}`는 **`RevocationSnapshot` 객체 그대로**를 돌려준다(래핑 없음, `cert` 포함). 형식과 검증은 [[docs/contracts/market-trust|신뢰 형식]] §6이다.

- `ETag: "<sequence>"`. `If-None-Match`에 같은 값을 보내면 `304`.
- `latest`는 `Cache-Control: public, max-age=60`, 번호를 지정한 스냅숏은 `public, max-age=31536000, immutable`(발행된 스냅숏은 바뀌지 않는다).
- `sequence`는 1부터 단조 증가한다. 번호가 정수가 아니거나 1 미만이면 `400 BAD_REQUEST`, 없으면 `404 NOT_FOUND`.
- 클라이언트는 받은 스냅숏을 `AcceptSnapshot`으로 검증해, 이미 가진 것보다 낮은 `sequence`는 거부한다(되감기 방어).

#### 발행 규칙

- 서버는 새 게시, yank, 운영자 폐기가 생길 때, 그리고 스냅숏 TTL의 절반마다 새 스냅숏을 발행한다. `sequence`는 항상 직전 값 +1이다.
- `expiresAt = issuedAt + TTL`이다. 기본 TTL은 7일이다. 만료는 *새 설치*를 멈추게 하는 신호이고, 이미 설치된 것을 멈추는 이유가 아니다([[docs/contracts/market-trust|신뢰 형식]] §6).
- `latest`는 live이면서 폐기되지 않은 버전 중 최고 버전이다. 그런 버전이 없으면 그 id는 `latest`에서 빠진다.
- `reason`: `publisher`는 퍼블리셔의 yank로만 생긴다. `security`와 `policy`는 서버 운영자의 로컬 조작으로만 생기며, **이를 만드는 HTTP 엔드포인트는 없다.**

## 4. 퍼블리셔 API (Bearer)

### 4.1 등록과 키

```
POST /market/v1/publishers
  { "name": "ACME", "contact": "ops@acme.example" }
→ 201 { "publisher_id": "pub_…", "token": "tmk_…", "created_at": "…" }
```

`name`은 필수(120자 이하), `contact`는 선택(200자 이하). 이 요청만 인증이 필요 없다. `token`은 **이 응답에서 한 번만** 보인다. 이후 모든 퍼블리셔 요청은 `Authorization: Bearer <token>`을 보낸다.

```
POST   /market/v1/publisher/keys
  { "public_key": "<base64url raw ed25519 공개키>", "label": "ci",
    "proof": "<base64url raw 서명>" }
→ 201 { "key_id": "<keyId>", "state": "active" }
GET    /market/v1/publisher/keys    → { "keys": [ { "key_id", "label", "state": "active|retired", "created_at" } ] }
DELETE /market/v1/publisher/keys/{key_id}   → 204
```

- `key_id`는 [[docs/contracts/market-trust|신뢰 형식]] §1의 keyId다.
- **소유 증명**: `proof`는 공개키에 대응하는 개인키로 ASCII 문자열 `terra-market-key-register:<publisher_id>:<key_id>`에 한 ed25519 서명이다. 검증 실패는 `422 SIGNATURE_INVALID`, 이미 등록된 키는 `409 KEY_EXISTS`.
- `DELETE`는 키를 `retired`로 바꾼다. 이미 게시된 버전은 그대로이고, 새 업로드만 그 키로 서명할 수 없다. 모르는 키는 `404`.

### 4.2 접두사 소유 증명

```
POST /market/v1/publisher/prefixes/claims
  { "prefix": "com.acme", "method": "dns-txt" }       // 또는 { "prefix": "io.github.alice", "method": "github" }
→ 201 { "claim_id": "clm_…", "state": "pending", "prefix": "com.acme", "method": "dns-txt",
        "challenge": { "record_name": "_terra-market.acme.com", "record_type": "TXT",
                       "record_value": "terra-market-verify=<publisher_id>:<nonce>" },
        "expires_at": "…" }
→ 201 (github) { …, "challenge": { "authorize_url": "https://github.com/login/oauth/authorize?…" }, … }

POST /market/v1/publisher/prefixes/claims/{claim_id}/verify
→ 200 { "claim_id": "…", "state": "verified|pending|failed", "prefix": "…", "reason": "…" }

GET  /market/v1/publisher/prefixes
→ 200 { "prefixes": [ { "prefix": "com.acme", "state": "verified", "verified_at": "…" } ] }
```

- `dns-txt`: 도메인은 접두사를 뒤집은 것(`com.acme` → `acme.com`), 레코드 이름은 `_terra-market.<domain>`, 값은 `terra-market-verify=<publisher_id>:<nonce>`다. `verify`가 TXT를 조회해 값이 있으면 `verified`, 아직 없으면 `pending`이다. `io.github.*` 접두사는 DNS로 증명할 수 없다.
- `github`: 서버 운영자가 켠 경우에만 동작한다. 꺼져 있으면 claim은 `400 BAD_REQUEST`("unsupported proof method"), 콜백은 `503 UNAVAILABLE`이다. 흐름은 아래와 같다.
  1. claim 응답의 `authorize_url`로 사람이 GitHub에 로그인한다(`state` = `<publisher_id>:<nonce>`).
  2. GitHub이 `GET /market/v1/auth/github/callback?code=&state=`로 돌려보낸다. 서버는 **교환하지 않고** 확인 페이지(HTML)만 보여 준다(퍼블리셔 이름·id·접두사 표시).
  3. 사람이 확인 버튼을 누르면 `POST /market/v1/auth/github/callback`(폼 `state`, `code`)이 코드를 교환해 GitHub login을 증거로 **한 번만** 기록한다(토큰은 저장하지 않는다). 응답: `{ "claim_id", "state": "pending", "github_login", "next" }`.
  4. 퍼블리셔가 `verify`를 호출한다. login이 `io.github.<login>`과 맞으면 `verified`, 다르면 `failed`.
  - 확인 단계가 있는 이유: `state`는 콜백을 claim에 묶지만 claim을 만든 퍼블리셔의 브라우저에는 묶지 않는다. 이미 앱을 승인한 사용자는 GitHub 동의 화면이 생략되므로, 확인 없이 교환하면 남이 `io.github.<피해자>`를 claim하고 피해자에게 링크를 보내 피해자 로그인으로 증명할 수 있다. 확인 페이지는 프레임을 금지하고(`frame-ancestors 'none'`, `X-Frame-Options: DENY`), `Referrer-Policy: no-referrer`, `Cache-Control: no-store`를 보낸다.
  - 콜백 오류: 잘못된 `state`·`code` 누락·GitHub 승인 거부·교환 실패 `400 BAD_REQUEST`, 없는 claim 또는 github 방식이 아닌 claim `404 NOT_FOUND`, 이미 증거를 받은 claim `409 CLAIM_CLOSED`, 만료 `410 CLAIM_EXPIRED`.
- claim은 만료된다(기본 24시간). 만료된 claim의 `verify`는 `failed`("claim expired")를 돌려준다.
- 접두사가 예약(`io.terra` 및 그 아래)이면 claim이 `403 PREFIX_RESERVED`, 다른 퍼블리셔의 접두사와 겹치면 `409 PREFIX_NOT_OWNED`다. 이미 다른 퍼블리셔가 검증한 접두사를 `verify`하면 `409 PREFIX_NOT_OWNED`.

### 4.3 업로드

```
PUT /market/v1/publisher/packages
  Content-Type: application/zip
  X-Terra-Digest: sha256:<hex>                       (선택: 사전 선언)
  X-Terra-Conformance-Report: <base64url(JSON)>      (선택: 자가 보고, §4.5)
  본문: .tmod 바이트

→ 200 { "digest": "sha256:…", "module_id": "…", "version": "…", "size_bytes": 123, "state": "accepted",
        "checks": [ { "name": "inspect", "status": "pass" },
                    { "name": "prefix_owner", "status": "pass" },
                    { "name": "author_signature", "status": "pass" },
                    { "name": "permissions_declared", "status": "pass" },
                    { "name": "conformance", "status": "self_reported|not_reported", "detail": "…" } ] }
→ 422 { "error": { "code": "…", "message": "…", "details": { "checks": [ … ] } } }
```

서버가 하는 검사(전부 통과해야 `accepted`):

1. **inspect** — `pkgformat.InspectPackage` 2단 판정과 매니페스트 읽기. 실패는 `INVALID_PACKAGE`.
2. **prefix_owner** — `module_id`를 덮는 검증된 접두사가 이 퍼블리셔 소유여야 한다. 예약이면 `PREFIX_RESERVED`, 소유 없음/다른 퍼블리셔면 `PREFIX_NOT_OWNED`. 한 id를 처음 게시한 퍼블리셔가 그 id를 계속 가진다.
3. **author_signature** — 매니페스트의 `signature.keyId`가 이 퍼블리셔의 **활성** 키이고, 작성자 서명([[docs/contracts/market-trust|신뢰 형식]] §2)이 맞아야 한다. 서명이 없거나 알고리즘이 `ed25519`가 아니거나 형식이 틀리면 `SIGNATURE_INVALID`, 키가 이 퍼블리셔의 활성 키가 아니면 `KEY_NOT_REGISTERED`.
4. **permissions_declared** — `permissions` 블록이 있어야 하고(없으면 `PERMISSIONS_MISSING`) 정규화되어야 한다(아니면 `INVALID_PACKAGE`).
5. **compatibility** — 매니페스트에 `compatibility`가 있으면 형식만 본다: `wireContract`는 1 이상의 정수, `core`는 비어 있지 않은 문자열. 틀리면 `INVALID_PACKAGE`.
6. **불변성** — 이미 게시된 id+version이 같은 digest가 아니면 `VERSION_EXISTS`.
7. **conformance** — 판정하지 않는다(§4.5).

그 밖의 규칙:

- 쿼터는 본문을 읽기 전에 본다: 퍼블리셔당 최근 24시간 업로드 수·바이트 한도를 넘으면 `429 QUOTA_EXCEEDED`. 기본값은 50개, 512 MiB이다.
- 크기 한도는 `/info`의 `limits.max_package_bytes`(기본 64 MiB). 넘으면 `413 PACKAGE_TOO_LARGE`.
- `X-Terra-Digest`가 본문의 sha256과 다르면 `400 DIGEST_MISMATCH`.
- 접수된 패키지는 아직 공개되지 않는다. §4.4의 게시를 해야 카탈로그에 오른다.

### 4.4 게시, 회수, 조회

```
POST /market/v1/publisher/publications
  { "digest": "sha256:…", "targets_extra": { "windows-amd64": "sha256:…" } }     // targets_extra는 선택
→ 201 { "publication_id": "…", "module_id": "…", "version": "…",
        "targets": { "any": "sha256:…", "windows-amd64": "sha256:…" },
        "chain": [ { …마켓 증명… } ] }
```

- `digest`는 이 퍼블리셔가 접수시킨 업로드여야 한다(아니면 `404 NOT_FOUND`). 그 패키지가 `any` target이 된다.
- `targets_extra`의 각 digest는 **같은 id와 version의 접수된 업로드**여야 한다(아니면 `422 BAD_TARGET`). `any`라는 이름은 쓸 수 없다(`400`).
- 게시 시점에 접두사 소유와 키 상태를 다시 본다(업로드와 게시 사이에 바뀔 수 있다). 같은 id+version이 이미 있으면 `409 VERSION_EXISTS`.
- 서버는 저장된 매니페스트에 마켓 증명을 붙여 `chain`으로 돌려준다. 새 스냅숏이 곧바로 발행된다.

```
POST /market/v1/publisher/publications/{publication_id}/yank
  { "reason": "…" }
→ 200 { "publication_id": "…", "state": "yanked", "snapshot_sequence": 13 }

GET  /market/v1/publisher/modules
→ 200 { "modules": [ { "publication_id", "module_id", "version", "state": "live|yanked", "targets": { … }, "published_at" } ] }
```

- yank는 그 버전을 폐기 스냅숏에 `reason: "publisher"`로 올린다. live가 아니면 `409 CONFLICT`, 남의 게시물이나 없는 id는 `404 NOT_FOUND`.
- yank된 버전은 `latest`에서 빠지고, 설치된 노드에서는 경고만 일으킨다(`publisher` 사유는 실행을 멈추지 않는다 — [[docs/contracts/market-trust|신뢰 형식]] §6).

### 4.5 적합성 자가 보고

서버는 퍼블리셔 코드를 **실행하지 않는다.** 퍼블리셔가 자기 기계에서 [[docs/guide/conformance|적합성 키트]]를 돌리고, 결과를 요약해 업로드 헤더로 붙일 수 있다.

```
X-Terra-Conformance-Report: base64url(JSON)       // 인코딩 후 4096바이트 이내, 패딩 없음
{ "status": "pass|fail|skipped", "kit": "terra-conformance", "target": "linux-amd64", "detail": "…" }
```

- `status`는 필수이고, 모르는 필드는 거부한다. 잘못된 모양은 본문을 읽기 전에 `400 BAD_REQUEST`.
- 서버는 모양만 확인해 그대로 저장하고, 카탈로그에 `source: "self_reported"`, `signed: false`로 보인다. **자가 보고 `fail`도 업로드를 막지 않는다.** 신뢰, 증명, 설치 판정의 입력이 아니다.
- `kit`은 100자, `target`은 64자, `detail`은 500자로 잘린다.

## 5. 클라이언트가 지켜야 할 것

- 카탈로그 응답의 `permissions`, `revoked`, `conformance`는 표시용이다. 설치 판정은 ① `chain`의 마켓 증명 검증, ② 서명된 매니페스트에서 계산한 권한, ③ 검증된 폐기 스냅숏으로 한다.
- 패키지는 받은 바이트의 sha256을 요청한 digest와 대조하고, 패키지 안 매니페스트가 `versions/{version}` 응답의 `manifest`와 같은지 확인한다.
- 마켓이 응답하지 않는 것(연결 실패, 5xx)과 마켓이 "없다"고 한 것(`404`)은 다른 일이다. 앞의 것은 재시도할 수 있는 장애이고, 아무것도 판정하지 않은 것이다.
- 신선한 스냅숏이 없을 때의 설치 정책은 [[docs/contracts/market-trust|신뢰 형식]] §6의 `MarketGate`가 정한다.

## 6. 인증 토큰과 비밀

- 토큰은 `tmk_`로 시작하는 불투명 문자열이다. 접두는 구현 세부이며 클라이언트가 의존하면 안 된다.
- 토큰과 개인키는 URL, 로그, 셸 기록에 남기지 않는다. 퍼블리셔 도구는 토큰을 환경변수(`TERRA_MARKET_TOKEN`)나 0600 파일에서만 읽는 것을 권장한다. 사용 방법은 [[docs/guide/publisher-guide|퍼블리셔 가이드]]를 본다.

## 7. 기대되는 서버 환경 (참고)

서버의 환경변수나 운영 방법은 이 계약이 아니다. 다만 **클라이언트가 볼 수 있는 동작**을 정하는 값이 있다: 패키지 크기 한도(`/info`의 `limits`), 스냅숏 TTL(스냅숏의 `expiresAt`), 쿼터(`429`), 증명 방식의 켜짐 여부(`github`). 이 값들은 서버마다 다를 수 있으므로 클라이언트는 상수로 가정하지 않는다.

## 8. 계약을 지키는지 확인하기

- 서명 관련 필드(`chain`, `cert`, 폐기 스냅숏)는 [`trust/testdata/market_vectors.json`](../../trust/testdata/market_vectors.json) 벡터와 `trust` 패키지 시험으로 확인한다.
- 이 문서의 JSON 모양은 호환 서버·클라이언트의 종단 시험으로 확인한다. 이 레포는 서버를 포함하지 않는다.

## 9. 변경 규칙

- 경로, 필드 이름, 오류 코드 문자열, 상태 코드는 **추가만** 한다. 기존 값의 의미를 바꾸거나 지우지 않는다.
- 응답에 필드를 더하는 것은 MINOR, 새 `api_version`(`/info`)은 호환되지 않는 변경이 필요할 때만 올리고 새 베이스 경로(`/market/v2`)로 둔다.
- 서명 payload에 닿는 변경은 [[docs/contracts/market-trust|신뢰 형식]] §8을 따른다.
