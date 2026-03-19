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
step "1/5" "PostgreSQL 설치 및 서비스 시작"

if ! command -v brew &>/dev/null; then
  die "Homebrew가 설치되어 있지 않습니다. https://brew.sh 에서 먼저 설치해주세요."
fi

# 설치된 postgresql 버전 감지 (postgresql@15, postgresql@16 등)
PG_FORMULA=$(brew list --formula 2>/dev/null | grep -E '^postgresql(@[0-9]+)?$' | tail -1)

if [[ -z "$PG_FORMULA" ]]; then
  echo "  PostgreSQL 설치 중 (postgresql@15)..."
  brew install postgresql@15
  PG_FORMULA="postgresql@15"
  # PATH에 psql 추가
  export PATH="$(brew --prefix $PG_FORMULA)/bin:$PATH"
else
  ok "이미 설치됨: $PG_FORMULA"
fi

# psql이 PATH에 없으면 추가
if ! command -v psql &>/dev/null; then
  export PATH="$(brew --prefix $PG_FORMULA)/bin:$PATH"
fi

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
step "2/5" "DB 유저(gateway) 및 데이터베이스(gateway) 생성"

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
step "3/5" "컨트랙트 의존성 설치 및 컴파일"

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
step "4/5" "컨트랙트 배포"

DEPLOY_PRIVATE_KEY=$(grep 'AdminPrivateKey' "$GATEWAY_CONFIG" \
  | sed 's/.*"\(0x[^"]*\)".*/\1/')
DEPLOY_RPC_URL=$(grep 'BlockchainRPCURL' "$GATEWAY_CONFIG" \
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
step "5/5" "config.go 컨트랙트 주소 업데이트"

# macOS sed는 -i '' 필요
sed -i '' \
  "s|FiatManagerAddr:.*|FiatManagerAddr:  \"$FIAT_MANAGER_PROXY\",|" \
  "$GATEWAY_CONFIG"

sed -i '' \
  "s|FiatTokenAddr:.*|FiatTokenAddr:    \"$FIAT_TOKEN\",|" \
  "$GATEWAY_CONFIG"

ok "config.go 업데이트 완료"

# ──────────────────────────────────────────────────────────────────
# 완료 요약
# ──────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "${GREEN}  셋업 완료!${NC}"
echo -e "${GREEN}════════════════════════════════════════${NC}"
echo -e "  PostgreSQL DB  : gateway (user: gateway / pw: secret)"
echo -e "  FiatManagerProxy : ${CYAN}$FIAT_MANAGER_PROXY${NC}"
echo -e "  FiatToken        : ${CYAN}$FIAT_TOKEN${NC}"
echo ""
echo -e "  게이트웨이 서버 실행:"
echo -e "  ${YELLOW}cd gateway && go run .${NC}"
echo ""
