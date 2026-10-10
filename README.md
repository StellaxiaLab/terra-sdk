---
title: "terra-sdk"
doc_type: "readme"
scope: "project"
target: "terra-sdk"
status: "draft"
last_updated: "2026-10-09"
---

# terra-sdk

Terra 모듈이 호스트와 대화하는 약속을 구현한 얇은 공개 Go SDK.

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
| `github.com/StellaxiaLab/terra-sdk/testwait` | 테스트용 대기 도우미(`Until` 등, 예산은 `TERRA_TEST_WAIT_SCALE`로 조절) |
| `github.com/StellaxiaLab/terra-sdk/conformance` | 적합성 테스트 키트 (`terra-conformance` 명령 포함) |

Go가 아닌 모듈도 [계약 문서](docs/contracts/module-host-http-contract.md)와 [Python 예제](docs/contracts/examples/README.md)만으로 만들 수 있고,
[적합성 테스트 키트](docs/guide/conformance.md)로 검사한다. 공개 심볼은 [목록](docs/reference/public-symbols.md), 원본 위치는 [출처 대응표](docs/reference/provenance.md), 버전 규칙은 [정책 초안](docs/policy/versioning.md)에 있다.

## 개발

```sh
go build ./... && go test -count=1 ./...
go vet ./...
```

CI(GitHub Actions)는 `go build`, `go vet`, `go test -count=1 ./...`를 Linux·Windows·macOS에서 돌린다. `v*` 태그를 올리면 릴리스 워크플로가 같은 검증 후 Release를 만든다.

## 라이선스

아직 지정하지 않았다. 레포 소유자가 정한다.
