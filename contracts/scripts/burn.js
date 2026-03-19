const { ethers } = require("hardhat");

// ────────────────────────────────────────────────────────────────────────────
// burn.js: EIP-2612 permit 서명 후 Gateway /api/v1/withdraw 호출
//
// 사용 방법 (setup.sh 실행 후):
//   SAMPLE_PRIVATE_KEY=<key>  \
//   SAMPLE_USER_ID=<id>       \
//   FIAT_TOKEN=<addr>         \
//   FIAT_MANAGER_PROXY=<addr> \
//   DEPLOY_RPC_URL=<url>      \
//   [GATEWAY_URL=http://localhost:8080] \
//   [AMOUNT=10000]            \
//   npx hardhat run scripts/burn.js --network stablenet
// ────────────────────────────────────────────────────────────────────────────

// FiatToken 최소 ABI (permit 관련 조회)
const TOKEN_ABI = [
  "function name() view returns (string)",
  "function version() external pure returns (string)",
  "function nonces(address owner) view returns (uint256)",
  "function decimals() view returns (uint8)",
  "function balanceOf(address) view returns (uint256)",
];

async function main() {
  // ── 환경변수 ─────────────────────────────────────────────────────────────
  const privateKey = process.env.SAMPLE_PRIVATE_KEY;
  const userId     = process.env.SAMPLE_USER_ID;
  const tokenAddr  = process.env.FIAT_TOKEN;
  const proxyAddr  = process.env.FIAT_MANAGER_PROXY;
  const gatewayUrl = process.env.GATEWAY_URL || "http://localhost:8080";
  const amount     = parseInt(process.env.AMOUNT || "10000", 10);

  if (!privateKey || !userId || !tokenAddr || !proxyAddr) {
    throw new Error(
      "필요 환경변수: SAMPLE_PRIVATE_KEY, SAMPLE_USER_ID, FIAT_TOKEN, FIAT_MANAGER_PROXY\n" +
      "선택 환경변수: GATEWAY_URL (기본값: http://localhost:8080), AMOUNT (기본값: 10000)"
    );
  }

  // ── Provider & Wallet ────────────────────────────────────────────────────
  const provider = ethers.provider;
  const wallet   = new ethers.Wallet(privateKey, provider);
  console.log(`서명 계정  : ${wallet.address}`);
  console.log(`출금 금액  : ${amount}`);
  console.log(`Gateway URL: ${gatewayUrl}\n`);

  // ── 토큰 정보 조회 ────────────────────────────────────────────────────────
  const token = new ethers.Contract(tokenAddr, TOKEN_ABI, provider);

  const [tokenName, tokenVersion, nonce, decimals, balance] = await Promise.all([
    token.name(),
    token.version(),
    token.nonces(wallet.address),
    token.decimals(),
    token.balanceOf(wallet.address),
  ]);

  // Gateway(Go)는 amount × 10^decimals 을 컨트랙트에 넘기므로 permit도 같은 값으로 서명해야 함
  const contractAmount = BigInt(amount) * (BigInt(10) ** BigInt(decimals));

  console.log(`토큰       : ${tokenName} (version=${tokenVersion}, decimals=${decimals})`);
  console.log(`잔액       : ${ethers.formatUnits(balance, decimals)} ${tokenName}`);
  console.log(`출금 금액  : ${amount} (contractAmount=${contractAmount})`);
  console.log(`Nonce      : ${nonce}`);

  const network = await provider.getNetwork();
  const chainId = network.chainId;
  console.log(`Chain ID   : ${chainId}\n`);

  if (balance < contractAmount) {
    throw new Error(`잔액 부족: 보유=${ethers.formatUnits(balance, decimals)}, 요청=${amount}`);
  }

  // ── Permit 서명 (EIP-2612) ────────────────────────────────────────────────
  // value는 컨트랙트에 넘기는 contractAmount (= amount × 10^decimals)로 서명해야 함
  // permitDeadline: 10분 뒤
  const permitDeadline = Math.floor(Date.now() / 1000) + 60 * 10;

  const domain = {
    name:              tokenName,
    version:           tokenVersion,
    chainId:           chainId,
    verifyingContract: tokenAddr,
  };

  const types = {
    Permit: [
      { name: "owner",    type: "address" },
      { name: "spender",  type: "address" },
      { name: "value",    type: "uint256" },
      { name: "nonce",    type: "uint256" },
      { name: "deadline", type: "uint256" },
    ],
  };

  const message = {
    owner:    wallet.address,
    spender:  proxyAddr,
    value:    contractAmount,   // amount × 10^decimals — Go의 toContractAmount()와 동일
    nonce:    nonce,
    deadline: BigInt(permitDeadline),
  };

  console.log(`Permit 서명 중...`);
  console.log(`  spender  : ${proxyAddr}`);
  console.log(`  amount   : ${amount}`);
  console.log(`  deadline : ${permitDeadline} (${new Date(permitDeadline * 1000).toISOString()})`);

  const signature = await wallet.signTypedData(domain, types, message);
  console.log(`  signature: ${signature}\n`);

  // ── Gateway /api/v1/withdraw 호출 ─────────────────────────────────────────
  const url  = `${gatewayUrl}/api/v1/withdraw`;
  const body = {
    user_id:          userId,
    amount:           amount,
    permit_deadline:  permitDeadline,
    permit_signature: signature,
  };

  console.log(`Gateway 호출: POST ${url}`);
  console.log(JSON.stringify(body, null, 2));

  const res  = await fetch(url, {
    method:  "POST",
    headers: { "Content-Type": "application/json" },
    body:    JSON.stringify(body),
  });

  const text = await res.text();
  if (res.ok) {
    console.log(`\n✓ 출금 요청 성공 (HTTP ${res.status})`);
    try {
      console.log(JSON.stringify(JSON.parse(text), null, 2));
    } catch {
      console.log(text);
    }
  } else {
    console.error(`\n✗ 출금 요청 실패 (HTTP ${res.status})`);
    console.error(text);
    process.exit(1);
  }
}

main()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error(err);
    process.exit(1);
  });
