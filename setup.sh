#!/usr/bin/env bash
set -euo pipefail

# ══════════════════════════════════════════════════════════════════
#  my-project 로컬 환경 셋업 스크립트
#  - PostgreSQL 설치 및 DB/유저 생성
#  - 스마트 컨트랙트 컴파일 & 배포
#  - config.go 컨트랙트 주소 자동 업데이트
# ══════════════════════════════════════════════════════════════════

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONTRACTS_DIR="$SCRIPT_DIR/contracts"
GATEWAY_CONFIG="$SCRIPT_DIR/gateway/config/config.go"

step() { echo -e "\n${CYAN}[$1]${NC} $2"; }
ok()   { echo -e "${GREEN}  ✓ $1${NC}"; }
warn() { echo -e "${YELLOW}  ! $1${NC}"; }
die()  { echo -e "${RED}  ✗ $1${NC}"; exit 1; }

# ──────────────────────────────────────────────────────────────────
# 1. PostgreSQL 설치 및 서비스 시작
# ──────────────────────────────────────────────────────────────────
step "1/6" "PostgreSQL 설치 및 서비스 시작"

if ! command -v brew &>/dev/null; then
  die "Homebrew가 설치되어 있지 않습니다. https://brew.sh 에서 먼저 설치해주세요."
fi

PG_FORMULA="postgresql@15"

echo "  PostgreSQL 설치 중 ($PG_FORMULA)..."
brew install "$PG_FORMULA" || true

# PATH에 psql 추가
export PATH="$(brew --prefix $PG_FORMULA)/bin:$PATH"

# 서비스 시작
brew services start "$PG_FORMULA" &>/dev/null || true

# 준비 대기 (최대 15초)
echo "  PostgreSQL 준비 대기 중..."
RETRIES=15
until pg_isready -q 2>/dev/null; do
  RETRIES=$((RETRIES - 1))
  [[ $RETRIES -eq 0 ]] && die "PostgreSQL이 시작되지 않습니다."
  sleep 1
done
ok "PostgreSQL 실행 중"

# ──────────────────────────────────────────────────────────────────
# 2. DB 유저 및 데이터베이스 생성
# ──────────────────────────────────────────────────────────────────
step "2/6" "DB 유저(gateway) 및 데이터베이스(gateway) 생성"

# 유저 생성 (이미 있으면 무시)
psql postgres -tc "SELECT 1 FROM pg_roles WHERE rolname='gateway'" \
  | grep -q 1 \
  && warn "유저 'gateway' 이미 존재 - 건너뜀" \
  || (psql postgres -c "CREATE USER gateway WITH PASSWORD 'secret';" && ok "유저 'gateway' 생성 완료")

# 데이터베이스 생성 (이미 있으면 무시)
psql postgres -tc "SELECT 1 FROM pg_database WHERE datname='gateway'" \
  | grep -q 1 \
  && warn "데이터베이스 'gateway' 이미 존재 - 건너뜀" \
  || (psql postgres -c "CREATE DATABASE gateway OWNER gateway;" && ok "데이터베이스 'gateway' 생성 완료")

# ──────────────────────────────────────────────────────────────────
# 3. 컨트랙트 의존성 설치 및 컴파일
# ──────────────────────────────────────────────────────────────────
step "3/6" "컨트랙트 의존성 설치 및 컴파일"

cd "$CONTRACTS_DIR"

if [[ ! -d node_modules ]]; then
  echo "  npm install 중..."
  npm install
else
  warn "node_modules 이미 존재 - npm install 건너뜀"
fi

echo "  컴파일 중..."
npx hardhat compile --quiet
ok "컴파일 완료"

# ──────────────────────────────────────────────────────────────────
# 4. config.go에서 배포 설정 읽기
# ──────────────────────────────────────────────────────────────────
step "4/6" "컨트랙트 배포"

# 값 할당 라인만 매칭 (필드 선언 라인 제외하기 위해 ':' 포함)
DEPLOY_PRIVATE_KEY=$(grep 'AdminPrivateKey:' "$GATEWAY_CONFIG" \
  | sed 's/.*"\(0x[^"]*\)".*/\1/')
DEPLOY_RPC_URL=$(grep 'BlockchainRPCURL:' "$GATEWAY_CONFIG" \
  | sed 's/.*"\([^"]*\)".*/\1/')

if [[ -z "$DEPLOY_PRIVATE_KEY" ]]; then
  die "config.go에서 AdminPrivateKey를 읽을 수 없습니다."
fi
if [[ -z "$DEPLOY_RPC_URL" ]]; then
  die "config.go에서 BlockchainRPCURL를 읽을 수 없습니다."
fi

echo "  RPC URL: $DEPLOY_RPC_URL"
echo "  배포 계정: (config.go AdminPrivateKey)"
echo ""

# 배포 실행
DEPLOY_OUTPUT=$(
  DEPLOY_PRIVATE_KEY="$DEPLOY_PRIVATE_KEY" \
  DEPLOY_RPC_URL="$DEPLOY_RPC_URL" \
  npx hardhat run scripts/deploy.js --network stablenet 2>&1
)
echo "$DEPLOY_OUTPUT"

# 결과에서 주소 파싱
FIAT_MANAGER_PROXY=$(echo "$DEPLOY_OUTPUT" | grep '^FIAT_MANAGER_PROXY=' | cut -d= -f2 | tr -d '[:space:]')
FIAT_TOKEN=$(echo "$DEPLOY_OUTPUT"         | grep '^FIAT_TOKEN='         | cut -d= -f2 | tr -d '[:space:]')

if [[ -z "$FIAT_MANAGER_PROXY" || -z "$FIAT_TOKEN" ]]; then
  die "배포 결과에서 컨트랙트 주소를 파싱할 수 없습니다. 위 로그를 확인해주세요."
fi

# ──────────────────────────────────────────────────────────────────
# 5. config.go 컨트랙트 주소 자동 업데이트
# ──────────────────────────────────────────────────────────────────
step "5/6" "config.go 컨트랙트 주소 업데이트"

# macOS sed는 -i '' 필요
sed -i '' \
  "s|FiatManagerAddr:.*|FiatManagerAddr:  \"$FIAT_MANAGER_PROXY\",|" \
  "$GATEWAY_CONFIG"

sed -i '' \
  "s|FiatTokenAddr:.*|FiatTokenAddr:    \"$FIAT_TOKEN\",|" \
  "$GATEWAY_CONFIG"

ok "config.go 업데이트 완료"

# ──────────────────────────────────────────────────────────────────
# 6. 샘플 유저 DB 등록
# ──────────────────────────────────────────────────────────────────
step "6/6" "샘플 유저 DB 등록"

SAMPLE_USER_ID=$(grep 'SampleUserID:'    "$GATEWAY_CONFIG" | sed 's/.*"\([^"]*\)".*/\1/')
SAMPLE_ADDRESS=$(grep 'SampleAddress:'   "$GATEWAY_CONFIG" | sed 's/.*"\([^"]*\)".*/\1/')
SAMPLE_ACCOUNT=$(grep 'SampleAccountNo:' "$GATEWAY_CONFIG" | sed 's/.*"\([^"]*\)".*/\1/')

PG_DSN="host=localhost port=5432 user=gateway password=secret dbname=gateway sslmode=disable"

# users 테이블 생성 (없으면)
psql "$PG_DSN" <<SQL
CREATE TABLE IF NOT EXISTS users (
    user_id    VARCHAR(255) PRIMARY KEY,
    address    VARCHAR(255) NOT NULL UNIQUE,
    account_no VARCHAR(255) NOT NULL
);
SQL

# 샘플 유저 INSERT (이미 있으면 무시)
psql "$PG_DSN" -c \
  "INSERT INTO users (user_id, address, account_no)
   VALUES ('$SAMPLE_USER_ID', '$SAMPLE_ADDRESS', '$SAMPLE_ACCOUNT')
   ON CONFLICT (user_id) DO NOTHING;"

ok "샘플 유저 등록 완료 (user_id=$SAMPLE_USER_ID)"

# ──────────────────────────────────────────────────────────────────
# 완료 요약
# ──────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "${GREEN}  셋업 완료!${NC}"
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "  PostgreSQL DB    : gateway (user: gateway / pw: secret)"
echo -e "  FiatManagerProxy : ${CYAN}$FIAT_MANAGER_PROXY${NC}"
echo -e "  FiatToken        : ${CYAN}$FIAT_TOKEN${NC}"
echo -e "  샘플 유저"
echo -e "    user_id    : ${CYAN}$SAMPLE_USER_ID${NC}"
echo -e "    address    : ${CYAN}$SAMPLE_ADDRESS${NC}"
echo -e "    account_no : ${CYAN}$SAMPLE_ACCOUNT${NC}"
echo ""
echo -e "  게이트웨이 서버 실행:"
echo -e "  ${YELLOW}cd gateway && go run .${NC}"
echo ""
