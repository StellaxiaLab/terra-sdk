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

> 상태: 이전 작업 진행 중(v0.1.0 전). 설계와 작업 분담은 [[module-independent-repos|모듈 독립 레포 전환 설계]]를 본다. 파일 위치는 [docs/design/module-independent-repos.md](docs/design/module-independent-repos.md).

## 개발

```sh
go build ./... && go test -count=1 ./...
go vet ./...
```

CI(GitHub Actions)는 `go build`, `go vet`, `go test -count=1 ./...`를 Linux·Windows·macOS에서 돌린다. `v*` 태그를 올리면 릴리스 워크플로가 같은 검증 후 Release를 만든다.

## 라이선스

아직 지정하지 않았다. 레포 소유자가 정한다.
