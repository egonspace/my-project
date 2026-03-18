# 스테이블코인 발행·환수 게이트웨이 시스템

원화(KRW) 기반 스테이블코인(FiatToken)의 발행(Mint)과 소각(Burn)을 은행 거래와 연동하는 게이트웨이 시스템입니다.
사용자가 실제 은행에 원화를 입금하면 동일한 금액의 토큰이 블록체인 지갑에 발행되고, 반대로 토큰을 소각하면 연결된 은행 계좌로 원화가 송금됩니다.

---

## 목차

1. [전체 아키텍처](#전체-아키텍처)
2. [컴포넌트 개요](#컴포넌트-개요)
3. [스마트 컨트랙트](#스마트-컨트랙트)
4. [게이트웨이 서버](#게이트웨이-서버)
5. [데이터베이스 스키마](#데이터베이스-스키마)
6. [API 명세](#api-명세)
7. [요청 상태 흐름](#요청-상태-흐름)
8. [설정](#설정)
9. [빌드 및 실행](#빌드-및-실행)

---

## 전체 아키텍처

```
[사용자]
   │  ① 원화 입금 (은행)
   ▼
[퍼머뱅킹 (FirmBanking)]
   │  ② 입금 통보 → POST /api/v1/deposit
   ▼
┌──────────────────────────────────────────────┐
│              GatewayAPI (Gin HTTP)           │
│  - 요청 수신 및 상태 관리                      │
│  - StateDB (PostgreSQL) 기록                  │
│  - BlockchainClient → mintFromFiat() 호출    │
└──────────────────────────┬───────────────────┘
                           │ ③ mint Tx 제출
                           ▼
                  [EVM 블록체인]
                  FiatManager.sol
                  FiatToken.sol (ERC-20)
                           │ ④ FiatTokenMinted / FiatTokenBurnt 이벤트
                           ▼
┌──────────────────────────────────────────────┐
│           ChainListener (고루틴)              │
│  - WS 이벤트 구독 (자동 재연결)               │
│  - Mint 확정 → StateDB 업데이트               │
│  - Burn 확정 → FirmBanking.Transfer() 호출   │
│  - 타임아웃 폴링 → PENDING 초과 시 FAILURE    │
└──────────────────────────────────────────────┘
```

---

## 컴포넌트 개요

```
my-project/
├── contracts/              # 스마트 컨트랙트 (Solidity + Hardhat)
│   ├── FiatToken.sol       # ERC-20 스테이블코인 (UUPS 업그레이드 가능)
│   ├── FiatManager.sol     # 민트/소각 관리 컨트랙트
│   ├── hardhat.config.js   # Hardhat 빌드 설정
│   └── package.json        # OpenZeppelin 의존성
│
└── gateway/                # 게이트웨이 서버 (Go)
    ├── main.go             # 진입점 — 구성요소 초기화 및 실행
    ├── config/             # 환경 설정
    ├── api/                # HTTP 핸들러 (Gin)
    ├── blockchain/         # go-ethereum 기반 EVM 클라이언트
    ├── db/                 # PostgreSQL StateDB
    ├── firmbanking/        # 퍼머뱅킹 클라이언트 (현재 스텁)
    ├── listener/           # 블록체인 이벤트 리스너
    └── model/              # 공통 도메인 모델 및 상수
```

---

## 스마트 컨트랙트

### FiatToken.sol

UUPS 업그레이드 패턴을 적용한 ERC-20 스테이블코인입니다.

- `mint(address, uint256)` — FiatManager만 호출 가능
- `burn(uint256)` — FiatManager만 호출 가능
- `permit(...)` — EIP-2612 오프체인 서명 기반 승인
- `transferWithAuthorization(...)` — EIP-3009 서명 기반 전송
- `decimals()` — 토큰 소수점 자리수 (게이트웨이 기동 시 자동 조회)

### FiatManager.sol

FiatToken 발행·소각을 중개하는 관리 컨트랙트입니다. UUPS 업그레이드 패턴 적용.

| 함수 | 설명 |
|------|------|
| `mintFromFiat(_to, _amount, _expiration, _txId)` | 원화 입금 확인 후 토큰 발행. `_expiration` 초과 시 revert. |
| `burnForFiat(_owner, _amount, _permitDeadline, _permitSignature, _txId)` | permit 서명 검증 후 토큰 소각. 정수 단위만 허용. |
| `transferFrom(...)` | 서명 기반 토큰 이체 |
| `authorize(address)` / `deauthorize(address)` | 사용자 인가/해제 (admin 전용) |

**이벤트**

| 이벤트 | 설명 |
|--------|------|
| `FiatTokenMinted(uint256 indexed _txId, address indexed _minter, uint256 _amount)` | 토큰 발행 완료 |
| `FiatTokenBurnt(uint256 indexed _txId, address indexed _minter, uint256 _amount)` | 토큰 소각 완료 |

> `_txId`는 은행 거래번호(`bank_tx`)의 keccak256 해시값으로, 동일 거래의 중복 처리를 온체인에서 원천 차단합니다.

---

## 게이트웨이 서버

### blockchain/client.go

go-ethereum 기반 EVM 클라이언트입니다.

- **기동 시**: `decimals()` RPC 호출로 토큰 소수점 자리수를 조회하고 `decimalMultiplier`(10^decimal)를 캐시합니다.
- **Mint 송신**: 원화 금액 × `decimalMultiplier` → 컨트랙트 `_amount`로 변환 후 `mintFromFiat()` 트랜잭션 제출
- **이벤트 수신**: 컨트랙트 `_amount` ÷ `decimalMultiplier` → 원화 금액으로 역변환
- **트랜잭션 유형**: `header.BaseFee` 존재 시 EIP-1559(`DynamicFeeTx`), 없으면 Legacy(`LegacyTx`) 자동 선택
- **WS 구독**: 연결 끊김 시 5초 후 자동 재연결

### listener/chain_listener.go

블록체인 이벤트를 구독하고 후속 처리를 담당합니다.

- **FiatTokenMinted** 이벤트 수신 → 해당 request의 상태를 `SUCCESS`로 업데이트
- **FiatTokenBurnt** 이벤트 수신 → 사용자 계좌번호 조회 후 FirmBanking Transfer 호출, 성공 시 `SUCCESS` 업데이트
- **타임아웃 폴링** (10초 주기): `expiration` 이 지나도록 `PENDING` 상태인 요청을 `FAILURE(ErrorCode=TxTimeout)`으로 처리

> `expiration`은 `SendMintTx` 성공 시점에 설정되며, 블록체인에 아직 제출되지 않은 `REQUESTED` 상태의 요청은 타임아웃 폴링 대상에서 제외됩니다.

### api/handler.go

| 엔드포인트 | 설명 |
|-----------|------|
| `POST /api/v1/deposit` | 원화 입금 통보 수신, Mint 트랜잭션 제출 |
| `POST /api/v1/mint/:id/retry` | 실패한 Mint 요청 재시도 (`FAILURE` 상태만 허용) |
| `GET /api/v1/request/:id` | 요청 상태 조회 |

---

## 데이터베이스 스키마

### users

| 컬럼 | 타입 | 설명 |
|------|------|------|
| `user_id` | VARCHAR PK | 사용자 식별자 |
| `address` | VARCHAR UNIQUE | 블록체인 지갑 주소 |
| `account_no` | VARCHAR | 은행 계좌번호 (Burn 시 송금 대상) |

### requests

| 컬럼 | 타입 | 설명 |
|------|------|------|
| `id` | SERIAL PK | 요청 ID |
| `type` | INTEGER | 1=MINT, 2=BURN |
| `status` | INTEGER | 아래 상태 코드 참조 |
| `user_id` | VARCHAR FK | users.user_id 참조 |
| `bank_tx` | VARCHAR (nullable) | 은행 거래번호. BURN은 NULL. 존재 시 UNIQUE |
| `tx_hash` | VARCHAR | 블록체인 트랜잭션 해시 |
| `timestamp` | BIGINT | 요청 생성 시각 (UnixMilli) |
| `expiration` | BIGINT | Tx 제출 후 확정 기한 (Unix). 미제출 시 0 |
| `amount` | BIGINT | 원화 금액 (원 단위) |
| `error_code` | INTEGER | 아래 에러 코드 참조 |

**상태 코드**

| 값 | 이름 | 설명 |
|----|------|------|
| 1 | REQUESTED | 생성됨, 블록체인 미제출 |
| 2 | PENDING | Tx 제출 완료, 확정 대기 중 |
| 3 | SUCCESS | 블록체인 확정 완료 |
| -1 | FAILURE | 실패 (error_code 참조) |

**에러 코드**

| 값 | 이름 | 설명 |
|----|------|------|
| 0 | None | 정상 |
| 1 | TxFailed | 트랜잭션 제출 실패 |
| 2 | TxTimeout | 확정 대기 시간(3분) 초과 |
| 3 | BankingFailed | FirmBanking 송금 실패 |
| 4 | DuplicateRequest | 동일 bank_tx 중복 요청 |

---

## API 명세

### POST /api/v1/deposit

원화 입금 통보를 수신하고 Mint 트랜잭션을 제출합니다.

**요청 본문**
```json
{
  "user_id": "user-001",
  "bank_tx": "BK20240318000123",
  "amount": 10000
}
```

| 필드 | 설명 |
|------|------|
| `user_id` | 미리 등록된 사용자 ID |
| `bank_tx` | 은행 거래번호 (중복 제출 방지 키) |
| `amount` | 입금액 (원 단위, 소수점 없음) |

**응답 (200)**
```json
{
  "request_id": 42,
  "user_id": "user-001",
  "tx_hash": "0xabc...def",
  "status": "PENDING"
}
```

**오류 응답**

| HTTP | 원인 |
|------|------|
| 400 | 필드 누락 또는 미등록 사용자 |
| 409 | 동일 bank_tx 중복 요청 |
| 500 | DB 오류 또는 트랜잭션 제출 실패 |

---

### POST /api/v1/mint/:id/retry

실패한 Mint 요청을 재시도합니다. **`FAILURE` 상태일 때만** 허용됩니다.
`REQUESTED`, `PENDING`, `SUCCESS` 상태에서 호출하면 409를 반환합니다.

**응답 (200)**
```json
{
  "request_id": 42,
  "tx_hash": "0x123...789",
  "status": "PENDING"
}
```

---

### GET /api/v1/request/:id

요청의 현재 상태를 조회합니다.

**응답 (200)**
```json
{
  "id": 42,
  "type": 1,
  "status": 3,
  "user_id": "user-001",
  "bank_tx": "BK20240318000123",
  "tx_hash": "0xabc...def",
  "timestamp": 1710720000000,
  "expiration": 1710720180,
  "amount": 10000,
  "error_code": 0
}
```

---

## 요청 상태 흐름

```
[POST /deposit 수신]
        │
        ▼
   REQUESTED (DB insert, expiration=0)
        │
        │ SendMintTx 성공
        ▼
   PENDING (tx_hash, expiration=now+3분 설정)
        │
        ├─── FiatTokenMinted 이벤트 수신 ──▶ SUCCESS
        │
        └─── expiration 초과 (타임아웃 폴링) ──▶ FAILURE (TxTimeout)
                                                      │
                                              POST /mint/:id/retry
                                                      │
                                               PENDING → SUCCESS
```

---

## 설정

`gateway/config/config.go`의 기본값을 수정하거나, 환경 변수로 주입하십시오.

| 항목 | 기본값 | 설명 |
|------|--------|------|
| `ServerPort` | `:8080` | HTTP 서버 포트 |
| `DatabaseDSN` | `host=localhost ...` | PostgreSQL 연결 문자열 |
| `BlockchainRPCURL` | `http://localhost:8545` | EVM JSON-RPC 엔드포인트 |
| `BlockchainWSURL` | `ws://localhost:8546` | 이벤트 구독용 WebSocket 엔드포인트 |
| `FiatManagerAddr` | — | FiatManager 컨트랙트 주소 |
| `FiatTokenAddr` | — | FiatToken 컨트랙트 주소 (decimals 조회에 사용) |
| `AdminPrivateKey` | — | Mint Tx 서명용 관리자 지갑 개인키 (0x 포함) |
| `FirmBankingURL` | `http://firmbanking.internal` | 퍼머뱅킹 API 베이스 URL |

---

## 빌드 및 실행

### 사전 준비

- Go 1.21+
- Node.js 18+ (컨트랙트 빌드 시)
- PostgreSQL 14+
- EVM 호환 블록체인 노드 (HTTP RPC + WebSocket)

### 컨트랙트 빌드

```bash
cd contracts
npm install
npx hardhat compile
```

빌드 결과물은 `contracts/artifacts/` 에 생성됩니다.

### 게이트웨이 서버 실행

```bash
cd gateway
go mod tidy
go run main.go
```

서버 기동 시 다음이 자동으로 수행됩니다.

1. PostgreSQL 연결 및 테이블/인덱스 생성
2. FiatToken 컨트랙트에서 `decimals()` 조회
3. FiatManager 이벤트 WebSocket 구독 시작
4. 타임아웃 폴링 고루틴 시작 (10초 주기)
5. HTTP API 서버 시작 (`:8080`)

### 종료

`Ctrl+C` 또는 `SIGTERM` 신호를 보내면 모든 고루틴과 DB 연결이 정상 종료됩니다.
