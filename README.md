# my-project

원화(KRW) ↔ 스테이블코인(FiatToken) 게이트웨이 서버.
은행 입금 시 토큰을 발행(Mint)하고, 출금 요청 시 토큰을 소각(Burn)하여 은행 송금을 처리합니다.

## 아키텍처

```
FirmBanking(은행)
     │  POST /deposit
     ▼
┌─────────────────────────────────────────────────┐
│                 Gateway Server :8080            │
│  API (Gin) ── StateDB (PostgreSQL) ── Listener  │
└────────────────────┬────────────────────────────┘
                     │ mintFromFiat / burnForFiat
                     ▼
              StableNet (EVM)
         FiatManagerProxy ── FiatToken
```

**배포된 컨트랙트 (StableNet Testnet)**

| | 주소 |
|---|---|
| FiatManagerProxy | `0xa2fBfFaC5DAc0883e33aeBecA95976e4f9c31A51` |
| FiatToken        | `0x24264271d5A489e5791b643016B3215a74e117E1` |

Explorer: https://explorer.stablenet.network

---

## 사전 요구사항

| 도구 | 비고 |
|---|---|
| Go 1.21+ | |
| Node.js 18+ | fetch 내장 버전 |
| npm 9+ | |
| Homebrew | macOS, PostgreSQL 설치용 |

---

## 1. 셋업

```bash
./setup.sh
```

자동으로 수행되는 작업:

1. PostgreSQL 15 설치 및 시작
2. DB 유저(`gateway`) / 데이터베이스(`gateway`) 생성
3. 컨트랙트 의존성 설치 및 컴파일
4. 컨트랙트 배포 (매번 새로 배포)
5. `gateway/config/config.go` 컨트랙트 주소 자동 업데이트
6. 샘플 유저 DB 등록

> 기존 컨트랙트 주소가 온체인에 있으면 배포를 건너뛰려면: `./setup.sh --no_deploy`

---

## 2. 서비스 실행

터미널 두 개를 열어 각각 실행합니다.

**터미널 1 — Mock FirmBanking 서버 (포트 8081)**

```bash
cd mock_firmbanking && go run .
```

**터미널 2 — Gateway 서버 (포트 8080)**

```bash
cd gateway && go run .
```

---

## 3. Mint (입금 → 토큰 발행)

### 입금 요청

```bash
curl -s -X POST http://localhost:8080/api/v1/deposit \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "sample-user-001",
    "bank_tx": "BANK-TX-001",
    "amount":  10000
  }' | jq
```

**응답**

```json
{
  "request_id": 1,
  "user_id": "sample-user-001",
  "tx_hash": "0xabc...",
  "status": "PENDING"
}
```

| 필드 | 설명 |
|---|---|
| `user_id` | 등록된 유저 ID |
| `bank_tx` | 은행 거래 고유번호 (중복 불가) |
| `amount`  | 금액 (원 단위) |

### 상태 조회

```bash
curl -s http://localhost:8080/api/v1/request/1 | jq
```

**상태값**

| status | 의미 |
|---|---|
| `1` REQUESTED | 요청 접수 (Tx 미제출) |
| `2` PENDING   | Tx 전송 완료, 체인 확정 대기 |
| `3` SUCCESS   | 완료 |
| `-1` FAILURE  | 실패 (`error_code` 참조) |

**error_code**

| error_code | 의미 |
|---|---|
| `1` | Tx 전송 실패 |
| `2` | 확정 타임아웃 (3분 초과) |
| `3` | FirmBanking 송금 실패 |

### 실패한 Mint 재시도

```bash
curl -s -X POST http://localhost:8080/api/v1/mint/1/retry | jq
```

FAILURE 상태인 요청만 재시도 가능합니다.

---

## 4. Burn (출금 → 토큰 소각 → 은행 송금)

출금은 EIP-2612 Permit 서명이 필요합니다. `burn.js`가 서명과 API 호출을 자동 처리합니다.

### burn.js 실행

```bash
cd contracts

SAMPLE_PRIVATE_KEY=0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d \
SAMPLE_USER_ID=sample-user-001 \
FIAT_TOKEN=0x24264271d5A489e5791b643016B3215a74e117E1 \
FIAT_MANAGER_PROXY=0xa2fBfFaC5DAc0883e33aeBecA95976e4f9c31A51 \
DEPLOY_RPC_URL=https://api.test.stablenet.network \
AMOUNT=10000 \
npx hardhat run scripts/burn.js --network stablenet
```

> `setup.sh` 완료 후 출력되는 명령어를 그대로 복사해서 사용할 수 있습니다.

**선택 환경변수**

| 변수 | 기본값 | 설명 |
|---|---|---|
| `GATEWAY_URL` | `http://localhost:8080` | Gateway 서버 주소 |
| `AMOUNT`      | `10000` | 출금 금액 (원 단위) |

### withdraw API 직접 호출 (참고)

`burn.js`가 내부적으로 호출하는 API입니다.
`permit_signature`는 EIP-712 Permit 서명값(0x 포함 hex)이어야 합니다.

```bash
curl -s -X POST http://localhost:8080/api/v1/withdraw \
  -H "Content-Type: application/json" \
  -d '{
    "user_id":          "sample-user-001",
    "amount":           10000,
    "permit_deadline":  1999999999,
    "permit_signature": "0x..."
  }' | jq
```

**응답**

```json
{
  "request_id": 2,
  "user_id": "sample-user-001",
  "tx_hash": "0xdef...",
  "status": "REQUESTED"
}
```

**Burn 상태 흐름**

```
[POST /withdraw]
      │
      ▼
 REQUESTED  ← burnForFiat Tx 전송 완료
      │
      │  FiatTokenBurnt 이벤트 수신
      ▼
 PENDING    ← FirmBanking 송금 중
      │
      ├── 송금 성공 ──▶ SUCCESS
      └── 송금 실패 ──▶ FAILURE (BankingFailed)
      └── Tx 타임아웃 ▶ FAILURE (TxTimeout)
```

---

## 5. DB 초기화

```bash
# requests, listener_state 초기화 (users 유지)
./clear.sh

# users까지 포함 전체 초기화
./clear.sh --full
```

---

## 6. 컨트랙트 스크립트

모든 스크립트는 `contracts/` 디렉토리에서 실행합니다.

### 배포

```bash
cd contracts

DEPLOY_PRIVATE_KEY=0x08c59f13ba871f16db690f25ade76e37db0609ca294c9e5ae9db58f4ba29b3ed \
DEPLOY_RPC_URL=https://api.test.stablenet.network \
npx hardhat run scripts/deploy.js --network stablenet
```

### 업그레이드 (FiatManager 구현체 교체)

컨트랙트 소스 수정 후 Proxy는 유지한 채 구현체만 교체합니다.

```bash
cd contracts

DEPLOY_PRIVATE_KEY=0x08c59f13ba871f16db690f25ade76e37db0609ca294c9e5ae9db58f4ba29b3ed \
DEPLOY_RPC_URL=https://api.test.stablenet.network \
FIAT_MANAGER_PROXY=0xa2fBfFaC5DAc0883e33aeBecA95976e4f9c31A51 \
npx hardhat run scripts/upgrade.js --network stablenet
```

### 유저 Authorize

```bash
cd contracts

DEPLOY_PRIVATE_KEY=0x08c59f13ba871f16db690f25ade76e37db0609ca294c9e5ae9db58f4ba29b3ed \
DEPLOY_RPC_URL=https://api.test.stablenet.network \
FIAT_MANAGER_PROXY=0xa2fBfFaC5DAc0883e33aeBecA95976e4f9c31A51 \
SAMPLE_ADDRESS=0x70997970C51812dc3A010C7d01b50e0d17dc79C8 \
npx hardhat run scripts/authorize.js --network stablenet
```

이미 authorize된 주소는 자동으로 건너뜁니다.

---

## 7. 샘플 유저

로컬 개발용 계정 (Hardhat 테스트 계정 #1).

| | |
|---|---|
| user_id     | `sample-user-001` |
| address     | `0x70997970C51812dc3A010C7d01b50e0d17dc79C8` |
| account_no  | `110-123-456789` |
| private_key | `0x59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d` |

---

## 8. 프로젝트 구조

```
my-project/
├── setup.sh                   # 로컬 환경 셋업 (PostgreSQL + 컨트랙트 배포)
├── clear.sh                   # DB 초기화
├── contracts/                 # 스마트 컨트랙트 (Solidity + Hardhat)
│   ├── FiatToken.sol
│   ├── FiatManager.sol
│   ├── FiatManagerProxy.sol
│   └── scripts/
│       ├── deploy.js          # 최초 배포
│       ├── upgrade.js         # 구현체 업그레이드
│       ├── authorize.js       # 유저 authorize
│       └── burn.js            # Permit 서명 + withdraw API 호출
├── mock_firmbanking/          # 가상 FirmBanking 서버 (포트 8081)
│   └── main.go
└── gateway/                   # 게이트웨이 서버 (포트 8080)
    ├── main.go
    ├── config/                # 설정 (config.go)
    ├── api/                   # HTTP 핸들러 (Gin)
    ├── blockchain/            # go-ethereum EVM 클라이언트
    ├── db/                    # PostgreSQL StateDB
    ├── firmbanking/           # FirmBanking HTTP 클라이언트
    ├── listener/              # 블록체인 이벤트 리스너
    └── model/                 # 도메인 모델 및 상수
```

---

## 9. 설정

`gateway/config/config.go` — `setup.sh` 실행 시 컨트랙트 주소가 자동 업데이트됩니다.

| 항목 | 기본값 |
|---|---|
| `ServerPort`       | `:8080` |
| `DatabaseDSN`      | `host=localhost port=5432 user=gateway password=secret dbname=gateway` |
| `BlockchainRPCURL` | `https://api.test.stablenet.network` |
| `BlockchainWSURL`  | `wss://ws.test.stablenet.network` |
| `FirmBankingURL`   | `http://localhost:8081` |
