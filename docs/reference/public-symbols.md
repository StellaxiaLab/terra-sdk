---
title: "공개 심볼 목록"
doc_type: "reference"
scope: "project"
target: "terra-sdk"
status: "draft"
last_updated: "2026-10-09"
related:
  - "[[docs/reference/provenance]]"
  - "[[docs/policy/versioning]]"
---

# 공개 심볼 목록

v0.1.0 준비 시점(`go doc` 기준 내보낸 식별자)의 목록이다. 시그니처와 설명은 `go doc github.com/StellaxiaLab/terra-sdk/<패키지>`를 본다.
[[docs/policy/versioning|버전 정책 초안]]에서 v0.x 동안의 깨짐 범위를 다룬다.

## `modulesdk` — 모듈이 호스트에 붙는 Go 도우미

- 타입: `Config`, `CoreClient`, `ReadinessFunc`, `SVIResourcesFunc`, `SVISink`, `SVISinkFunc`, `Server`
- 함수/메서드: `FromEnv`, `Listen`, `Server.Addr`, `Server.Close`, `Server.Serve`, `CoreFromEnv`, `NewCoreClient`, `CoreClient.Invoke`, `DataDir`, `ModuleConfig`, `LoadModuleConfig`
- 상수: `ConfigFileEnv`, `DataDirEnv`
- 오류: `ErrConfigNotProvided`, `ErrCoreUnavailable`, `ErrSVISinkNotServed`

## `modulert` — 호스트↔모듈 와이어 계약 (타입·상수·오류)

- 타입: `WorkloadIdentity`, `IdentityIssuer`, `HandshakeResponse`, `HandshakeResult`, `ReadinessResponse`, `Readiness`, `CoreInvocation`, `CoreResult`, `SVISinkOp`, `SVISinkRequest`, `SVISinkResponse`, `GatewayDelegateInput`, `GatewayDelegateOutput`, `SharedRoot`, `State`
- 함수/메서드: `IdentityFromEnv`, `WorkloadIdentity.Env`, `NewIdentityIssuer`, `IdentityIssuer.Issue`, `AllocateLoopbackEndpoint`, `Activate`, `NextActivationState`, `CanTransition`, `KnownState`, `IsTerminalStable`, `NextStates`, `ModuleDataDir`, `NameSharedRoots`, `PositionalSharedRootName`, `SharedRootPaths`, `SharedRoot.MarshalJSON`, `SharedRoot.UnmarshalJSON`
- 환경변수 이름: `EnvModuleID`, `EnvInstanceID`, `EnvEpoch`, `EnvNonce`, `EnvCredential`, `EnvEndpoint`, `EnvHostEndpoint`, `ConfigFileEnv`, `SharedRootsEnv`
- 경로·헤더: `HandshakePath`, `ReadinessPath`, `SVIResourcesPath`, `SVISinkPath`, `CoreInvokePath`, `CredentialHeader`, `TraceIDHeader`
- 위임 도어: `GatewayDelegateOperationID`, `GatewayDelegateOperationVersion`, `DelegatedCredentialPrefix`
- 그 밖의 상수: `LoopbackHost`, `DataDirName`, `SharedRootsPermission`, `SVISinkOpOpen`, `SVISinkOpWrite`, `SVISinkOpClose`, `State*` 9개(`StateDiscovered` … `StateStopped`)
- 오류: `ErrCoreOperationDenied`

## `svi` — SVI 데이터 타입과 서술자 검증

- 타입: `ResourceDescriptor`, `EndpointDescriptor`, `EndpointReference`, `RuntimeStatus`, `HandleRequest`, `SubjectRef`, `BindingDeclaration`, `BindingDesiredState`, `CompatibilityPolicy`, `Direction`, `Interaction`, `Operation`, `QoSProfile`, `ResourceStatus`, `RuntimeKind`, `RuntimeState`, `SubjectType`
- 상수 55개: 위 열거형의 값(`Direction*`, `Interaction*`, `Operation*`, `QoS*`, `Resource*`, `Runtime*`, `Subject*`, `CompatibilityPolicy*`, `BindingDesired*`)
- 검증(v0.2.0에서 추가): `ValidateResource`, `ValidateEndpoint`, `ParseSchemaRef`, 타입 `SchemaRef`, 오류 `ErrInvalidResource`, `ErrInvalidEndpoint`, `ErrInvalidSchemaRef`

## `protocol` — 입력 타입

- 타입: `InputPermissionTier`, `KeyEvent`, `PointerButton`, `PointerEvent`, `PointerPosition`
- 함수/메서드: `InputTierForKind`, `IsTerraVirtualInput`, `NormalizePixels`, `PointerPosition.ToPixels`
- 상수: `InputTierKeyboard`, `InputTierObserve`, `InputTierPointer`, `InputTierRaw`, `PointerButtonLeft`, `PointerButtonMiddle`, `PointerButtonRight`, `PointerPositionMax`, `VirtualInputBus`, `VirtualInputProduct`, `VirtualInputVendor`

## `testwait` — 테스트용 대기 도우미 (v0.2.0에서 추가)

- 함수: `Until`, `UntilFor`, `Became`, `BecameWithin`, `Budget`, `Scale`, `Context`, `ContextFor`
- 상수: `DefaultBudget`, `PollInterval`, `ScaleEnvVar`

## `conformance`, `conformance/conformancetest`, `cmd/terra-conformance` — 적합성 키트

- 타입: `Spec`, `CheckOptions`, `Identity`, `Report`, `Result`
- 함수: `Run`, `Check`, `NewIdentity`, `Identity.Env`, `Report.OK`, `Report.String`, `conformancetest.Run`
- 상수: 계약 리터럴 9개(`Env*` 6, `CredentialHeader`, `HandshakePath`, `ReadinessPath`)
- 명령: `terra-conformance`

사용 방법은 [[docs/guide/conformance|적합성 테스트 키트]]를 본다.
