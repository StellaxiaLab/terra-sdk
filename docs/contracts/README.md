---
title: "계약 문서 사본"
doc_type: "moc"
scope: "project"
target: "terra-sdk"
status: "draft"
last_updated: "2026-10-12"
---

# 계약 문서 사본

| 문서 | 내용 |
| --- | --- |
| [[docs/contracts/module-host-http-contract\|모듈 측 HTTP 계약]] | 호스트가 모듈에게 요구하는 전부: 환경변수 6 · 헤더 1 · 경로 2. Terra 원본의 사본 |
| [[docs/contracts/market-trust\|마켓 신뢰 형식 계약]] | 마켓과 노드가 공유하는 서명 payload 정규화·인증서·루트·폐기 스냅숏 형식. 사본이 아니라 이 레포가 정본이다 |
| [[docs/contracts/market-http-api\|마켓 HTTP 프로토콜 계약]] | 마켓 서버와 클라이언트가 주고받는 경로·요청·응답 JSON·오류 코드·캐시 규칙. 서명 형식은 신뢰 형식 계약을 가리킨다. 이 레포가 정본이다 |
| [[docs/contracts/wire-contract-version\|계약 버전 표지와 compatibility]] | `compat.ContractVersion`, manifest `compatibility`(`core`·`wireContract`)의 문법과 판정 규칙. 핸드셰이크 표지는 제안 |
| [[docs/contracts/examples/README\|최소 모듈 예제 (Python)]] | 표준 라이브러리만 쓴 71줄 모듈 |

모듈 측 HTTP 계약은 원본이 비공개 레포 Terra에 있고 이 폴더의 것은 **사본**이다. 어긋나면 Terra 쪽이 정본이다(계약 변경은 사용자 결정 사항). 마켓 신뢰 형식과 마켓 HTTP 프로토콜은 사본이 아니라 이 레포가 정본이다.
이 계약을 검사하는 도구는 [[docs/guide/conformance|적합성 테스트 키트]]다.
