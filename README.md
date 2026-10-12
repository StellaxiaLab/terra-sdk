---
title: "terra-sdk"
doc_type: "readme"
scope: "project"
target: "terra-sdk"
status: "draft"
last_updated: "2026-10-09"
---

# terra-sdk

Terra 모듈이 호스트와 대화하는 약속을 구현한 얇은 공개 Go SDK. v0.3.0부터 패키지 마켓과 노드가 공유하는 서명·패키지 형식(`trust`, `pkgformat`)도 담는다. 모듈은 이 둘을 import할 필요가 없다.

- Go 모듈 경로: `github.com/StellaxiaLab/terra-sdk` (단일 모듈)
- 외부 의존 0 (표준 라이브러리만)
- 최소 Go 버전: `go.mod`의 `go` 줄을 따른다

> 상태: 이전 작업(S-1~S-8) 반영, v0.1.0 태그 전. 설계와 작업 분담은 [docs/design/module-independent-repos.md](docs/design/module-independent-repos.md), 문서 목차는 [docs/README.md](docs/README.md)를 본다.

## 패키지

| import 경로 | 내용 |
| --- | --- |
| `github.com/StellaxiaLab/terra-sdk/modulesdk` | 모듈이 호스트에 붙는 도우미: `Listen/Serve`, `FromEnv`, `CoreClient`, `ModuleConfig`, `DataDir`, SVI sink |
| `github.com/StellaxiaLab/terra-sdk/modulert` | 호스트↔모듈 와이어 계약: 환경변수 이름, 경로, 헤더, 신원·핸드셰이크·코어 호출 DTO, 위임 도어, 공유 루트 |
| `github.com/StellaxiaLab/terra-sdk/svi` | SVI 데이터 타입과 서술자 검증(`ValidateResource` 등) |
| `github.com/StellaxiaLab/terra-sdk/protocol` | 입력 이벤트·권한·가상 입력 타입 |
| `github.com/StellaxiaLab/terra-sdk/trust` | **(v0.3.0, 불안정 v0.x)** 마켓·노드 공유 서명 형식: 작성자 서명·카운터서명·마켓 루트와 키 인증서·폐기 스냅숏과 그 검증기. 계약은 [market-trust](docs/contracts/market-trust.md) |
| `github.com/StellaxiaLab/terra-sdk/pkgformat` | **(v0.3.0, 불안정 v0.x)** 모듈 패키지 형식: Manifest v2·v1, 설정 스키마, 정규 권한 문자열, `.tmod` 아카이브와 한도. 마켓과 노드가 같은 판정을 하려고 쓴다 |
| `github.com/StellaxiaLab/terra-sdk/testwait` | 테스트용 대기 도우미(`Until` 등, 예산은 `TERRA_TEST_WAIT_SCALE`로 조절) |
| `github.com/StellaxiaLab/terra-sdk/conformance` | 적합성 테스트 키트 (`terra-conformance` 명령 포함) |

Go가 아닌 모듈도 [계약 문서](docs/contracts/module-host-http-contract.md)와 [Python 예제](docs/contracts/examples/README.md)만으로 만들 수 있고,
[적합성 테스트 키트](docs/guide/conformance.md)로 검사한다. 마켓에 모듈을 올리는 방법은 [퍼블리셔 가이드](docs/guide/publisher-guide.md), 마켓 프로토콜은 [HTTP 계약](docs/contracts/market-http-api.md)과 [신뢰 형식 계약](docs/contracts/market-trust.md)에 있다. 공개 심볼은 [목록](docs/reference/public-symbols.md), 원본 위치는 [출처 대응표](docs/reference/provenance.md), 버전 규칙은 [정책 초안](docs/policy/versioning.md)에 있다.

## 개발

```sh
go build ./... && go test -count=1 ./...
go vet ./...
```

CI(GitHub Actions)는 `go build`, `go vet`, `go test -count=1 ./...`를 Linux·Windows·macOS에서 돌린다. `v*` 태그를 올리면 릴리스 워크플로가 같은 검증 후 Release를 만든다.

## 라이선스

[Apache License 2.0](LICENSE). 외부 모듈 개발자가 자유롭게 링크하고 배포할 수 있게 하려는 것이 이 SDK의 목적이다. 특허 사용권이 명시되어 있고, 상표(“Terra” 등의 이름)는 이 라이선스가 허락하는 범위 밖이다.
