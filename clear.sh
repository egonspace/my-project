#!/usr/bin/env bash
set -euo pipefail

# ══════════════════════════════════════════════════════════════════
#  clear.sh — DB 초기화 스크립트
#
#  기본:   requests, listener_state 테이블 삭제 (users 유지)
#  --full: users 테이블까지 포함하여 전체 초기화
# ══════════════════════════════════════════════════════════════════

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m'

PG_DSN="host=localhost port=5432 user=gateway password=secret dbname=gateway sslmode=disable"

FULL=false
for arg in "$@"; do
  [[ "$arg" == "--full" ]] && FULL=true
done

echo -e "${CYAN}DB 초기화 중...${NC}"

if [[ "$FULL" == true ]]; then
  echo -e "${YELLOW}  ! --full 모드: users 포함 전체 초기화${NC}"
  psql "$PG_DSN" <<SQL
TRUNCATE TABLE requests, listener_state, users RESTART IDENTITY CASCADE;
SQL
  echo -e "${GREEN}  ✓ requests, listener_state, users 초기화 완료${NC}"
else
  psql "$PG_DSN" <<SQL
TRUNCATE TABLE requests, listener_state RESTART IDENTITY CASCADE;
SQL
  echo -e "${GREEN}  ✓ requests, listener_state 초기화 완료 (users 유지)${NC}"
fi
