---
title: "퍼블리셔 가이드 (공개 마켓에 모듈 올리기)"
doc_type: "guide"
scope: "project"
target: "terra-sdk"
status: "draft"
version: "0.2-draft"
last_updated: "2026-10-12"
related:
  - "[[docs/contracts/market-http-api]]"
  - "[[docs/contracts/market-trust]]"
  - "[[docs/guide/conformance]]"
  - "[[docs/policy/versioning]]"
---

# 퍼블리셔 가이드

공개 마켓에 모듈을 올리는 사람을 위한 안내다. 마켓 서버와 Terra CLI(`terra module publish --market` 등)는 이 레포 밖에 있으며,
이 문서는 **마켓이 무엇을 확인하고 무엇을 확인하지 않는지**, 그리고 올리는 순서를 설명한다.

> 상태: 초안. HTTP 경로와 오류 코드의 정본은 [[docs/contracts/market-http-api|마켓 HTTP 프로토콜 계약]], 서명 바이트 규칙과 인증서·폐기 형식의 정본은 [[docs/contracts/market-trust|마켓 신뢰 형식 계약]]이다.
> 이 문서는 둘을 다시 정하지 않는다. SDK는 Apache License 2.0이다([LICENSE](../../LICENSE)). 퍼블리셔 모듈의 라이선스는 퍼블리셔가 정한다.

## 1. 마켓이 보증하는 것과 하지 않는 것

마켓은 "이 패키지를 이 퍼블리셔가 냈고 바뀌지 않았다"만 보증한다. "이 노드에서 돌려도 된다"는 노드 주인의 동의(독립 노드) 또는 tree의 봉투(fleet)가 정한다.
따라서 마켓 패키지라도 core operation 권한이 자동으로 열리지 않고, 권한이 늘어난 새 버전은 설치 쪽 동의가 멈춘다. **권한은 매니페스트에 정직하게 선언한다.**

마켓은 **퍼블리셔 코드를 실행하지 않는다.** 서버는 적합성 키트를 돌리지 않으며(§4), 모듈이 안전하다는 심사도 하지 않는다. 수동 심사는 없다.

## 2. 준비물

| 항목 | 내용 |
| --- | --- |
| 퍼블리셔 계정 | 마켓에 등록한다(`terra market publisher register --name <이름>`). 등록 응답의 토큰은 **한 번만** 보인다 |
| 서명 키 | ed25519 키 쌍. 공개키를 계정에 등록하면 `keyId`가 된다(등록할 때 개인키로 소유 증명 서명을 만든다). **개인키는 퍼블리셔만 가진다.** CLI가 읽는 개인키 파일은 ed25519 개인키 64바이트를 base64로 인코딩한 한 줄이다 |
| 마켓 토큰 | 퍼블리셔 API용 Bearer 토큰. CLI는 환경변수 **`TERRA_MARKET_TOKEN`** 또는 `terra market login` 저장소에서만 읽는다(플래그로 받지 않는다 — 셸 기록 방지) |
| 접두사 | 모듈 id의 전역 접두사(예: `com.acme`, `io.github.<user>`). `io.terra.*`는 예약이라 쓸 수 없다 |

## 3. 접두사 소유 증명

접두사는 전역으로 하나의 소유자만 가진다. 증명 수단은 둘이다.

| 접두사 | 증명 |
| --- | --- |
| reverse-DNS (`com.acme`) | 그 도메인(`acme.com`)의 `_terra-market` 하위에 **DNS TXT 레코드**로 마켓이 발급한 값을 둔다 |
| `io.github.<user>` | **GitHub 로그인**으로 그 계정임을 증명한다. 마켓 서버가 이 방식을 켜 둔 경우에만 가능하다(기본 꺼짐) |

```sh
terra market prefix claim com.acme --method dns-txt   # 레코드 이름·값을 알려 준다
terra market prefix verify <claim_id>                 # 레코드를 올린 뒤 확인
```

id 규칙은 `<prefix>.` 경계 일치(`com.acme`는 `com.acme.tool`을 소유하지만 `com.acmex.tool`은 아니다)이고, **id는 불변**이다. 한 id를 처음 올린 퍼블리셔가 그 id를 계속 가진다.
claim은 만료된다(기본 24시간). 단계별 요청·응답은 [[docs/contracts/market-http-api|HTTP 프로토콜 계약]] §4.2.

## 4. 만들고 검사하기

1. 모듈을 [[docs/contracts/module-host-http-contract|계약]]대로 만든다(Go는 `modulesdk`, 그 밖의 언어는 계약 문서).
2. 매니페스트에 `permissions` 블록을 **반드시** 적는다. 비어 있어도 되지만 블록이 없으면 업로드가 거부된다(`PERMISSIONS_MISSING`).
3. (선택) `compatibility`를 적는다. 있으면 서버는 형식만 본다: `wireContract`는 1 이상의 정수, `core`는 비어 있지 않은 문자열이다.
4. 업로드 전에 [[docs/guide/conformance|적합성 키트]]를 로컬에서 돌린다:

```sh
terra-conformance -module-id <id> -operation /api/modules/<id>/v1/status -- ./my-module
echo $?   # 0 통과 · 1 규칙 위반 · 2 사용법/하네스 오류
```

**마켓은 이 키트를 실행하지 않는다.** 퍼블리셔 코드를 마켓 호스트에서 돌리지 않으려는 설계다. 결과를 마켓에 알리고 싶으면 퍼블리셔가 요약을 **자가 보고**로 붙인다(§5 6단계).
마켓은 모양만 확인해 저장하고, 카탈로그에 `conformance: {source:"self_reported", signed:false, …}`로 보인다. 자가 보고 `fail`도 업로드를 막지 않으며, 노드·tree는 이 값을 판정에 쓰지 않는다.

> 참고: 기계가 읽는 키트 보고서(`-json`)는 SDK v0.3.0에 아직 들어 있지 않다. Terra CLI의 `--conformance-report`는 그 보고서를 받으므로, 해당 옵션을 쓰려면 키트가 `-json`을 지원하는 버전이 필요하다.
> 그 전에는 `--conformance-report` 없이 올리면 된다(카탈로그에는 "보고 없음"으로 보인다).

## 5. 게시 절차

1. `terra market publisher register --name <이름>` → 토큰을 받는다. 이후 `TERRA_MARKET_TOKEN`으로 주거나 `terra market login`으로 저장한다.
2. `terra market key add --private-key <파일>`로 서명 키를 등록한다.
3. `terra market prefix claim <접두사> --method dns-txt|github` 후 `terra market prefix verify <claim_id>`로 접두사 소유를 증명한다(§3).
4. `terra module pack`으로 패키지(`.tmod`)를 만들고 저자 키로 서명한다 — 매니페스트에서 `signature`를 뺀 직렬화에 서명하며, 정확한 바이트 규칙은 [[docs/contracts/market-trust|신뢰 형식 계약]] §2다.
5. (선택) 적합성 결과를 요약한 보고서를 로컬에서 만든다(§4).
6. 업로드한다: `terra module publish <file.tmod> --market [--conformance-report <보고서>]`. 이 명령은 업로드 뒤 곧바로 게시까지 한다. 자가 보고는 `X-Terra-Conformance-Report` 헤더(base64url JSON, 4096바이트 이내)로 실린다.
7. 마켓이 자동 검사를 돌린다: 패키지 형식, 접두사 소유, 저자 서명 키가 그 퍼블리셔의 활성 키인지와 서명 유효성, `permissions` 선언 존재, 권한 블록의 정규화 가능 여부, 호환 필드 형식. **적합성은 판정하지 않는다.**
8. 통과하면 마켓이 `signature.chain`에 마켓 증명을 덧붙인다. 매니페스트 본문과 패키지 바이트는 바뀌지 않는다. 같은 id+version 재업로드는 `VERSION_EXISTS`로 거부된다(불변).
9. 카탈로그에 올라간다. 노드 주인은 권한 목록을 보고 동의한 뒤 설치한다.

토큰은 `TERRA_MARKET_TOKEN` 또는 `terra market login`으로만 준다. 마켓 주소는 인자, `--market-url`, `TERRA_MARKET_URL`, 로그인 저장값 순으로 찾는다.

흔한 거부 코드: `UNAUTHENTICATED` · `PREFIX_NOT_OWNED` · `PREFIX_RESERVED`(`io.terra.*`) · `KEY_NOT_REGISTERED` · `SIGNATURE_INVALID` · `PERMISSIONS_MISSING` · `INVALID_PACKAGE` · `PACKAGE_TOO_LARGE` · `VERSION_EXISTS` · `QUOTA_EXCEEDED`.
전체 목록과 HTTP 상태는 [[docs/contracts/market-http-api|HTTP 프로토콜 계약]] §1.2다.

**권한 표기.** 노드는 카탈로그의 `permissions`가 아니라 서명된 매니페스트의 `permissions` 블록에서 권한을 계산한다(카탈로그 값은 표시용). 정규 형식은 `kind:value`이며
`pkgformat.CanonicalPermissionsOf`가 계산한다:
`core.operation:<id>` · `storage:<v>` · `network:<v>` · `secret:<v>` · `native:<v>` (정렬·중복 제거). core operation의 id에는 `:`를 쓸 수 없고, 값이 빈 항목은 거부된다.
`network`·`secret`·`native`는 현재 런타임이 격리로 강제하지 않는다. 동의·승인 목록에는 올라가지만 보증은 아니다.

## 6. 버전을 올릴 때와 회수

- 권한을 늘리면 설치된 곳의 동의가 stale이 되어 업데이트가 멈춘다. 의도한 일인지 확인한다.
- 마켓 모듈은 자동 업데이트되지 않는다. 노드 주인이 다시 설치 절차(권한 확인과 동의)를 거쳐야 새 버전이 된다.
- 게시한 버전을 거두려면 yank한다(`POST …/publisher/publications/{publication_id}/yank`). 그 버전은 `latest`에서 빠지고, 서명된 폐기 스냅숏에 `publisher` 사유로 올라 설치된 노드에 경고가 뜬다. 실행을 멈추지는 않는다.
- `security` 또는 `policy` 사유의 폐기는 마켓 운영자의 로컬 조작으로만 생긴다. `security` 폐기는 설치된 것도 중지·격리한다. 키가 유출되면 운영자에게 즉시 알려 달라.
- 낮은 버전으로 되돌리는 배포는 노드의 다운그레이드 규칙으로 거부될 수 있다.
- 키를 바꾸려면 새 키를 등록하고 옛 키를 `DELETE /publisher/keys/{key_id}`로 retire한다. 이미 게시된 버전은 그대로고, 새 업로드만 옛 키로 서명할 수 없다.
