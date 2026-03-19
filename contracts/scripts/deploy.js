const { ethers } = require("hardhat");

async function main() {
  const [deployer] = await ethers.getSigners();
  console.log(`배포 계정: ${deployer.address}`);

  const balance = await ethers.provider.getBalance(deployer.address);
  console.log(`잔액: ${ethers.formatEther(balance)} ETH\n`);

  // ─── 토큰 파라미터 (환경변수로 오버라이드 가능) ─────────────────────
  const TOKEN_NAME     = process.env.TOKEN_NAME     || "KRW Stablecoin";
  const TOKEN_SYMBOL   = process.env.TOKEN_SYMBOL   || "KRWS";
  const TOKEN_CURRENCY = process.env.TOKEN_CURRENCY || "KRW";
  const TOKEN_DECIMALS = parseInt(process.env.TOKEN_DECIMALS || "18");
  const MAX_MINT       = ethers.MaxUint256;

  // ─── 1. FiatManager 구현체 배포 ────────────────────────────────────
  console.log("1/5  FiatManager(impl) 배포 중...");
  const FiatManager = await ethers.getContractFactory("FiatManager");
  const fiatManagerImpl = await FiatManager.deploy();
  await fiatManagerImpl.waitForDeployment();
  const implAddr = await fiatManagerImpl.getAddress();
  console.log(`     FiatManager(impl): ${implAddr}`);

  // ─── 2. FiatManagerProxy 배포 ──────────────────────────────────────
  console.log("2/5  FiatManagerProxy 배포 중...");
  const FiatManagerProxy = await ethers.getContractFactory("FiatManagerProxy");
  const proxy = await FiatManagerProxy.deploy(implAddr);
  await proxy.waitForDeployment();
  const proxyAddr = await proxy.getAddress();
  console.log(`     FiatManagerProxy:  ${proxyAddr}`);

  // ─── 3. FiatToken 배포 ─────────────────────────────────────────────
  // masterMinter = deployer (이후 proxy를 minter로 등록)
  // pauser / blacklister / owner = deployer
  console.log("3/5  FiatToken 배포 중...");
  const FiatToken = await ethers.getContractFactory("FiatToken");
  const fiatToken = await FiatToken.deploy(
    TOKEN_NAME,
    TOKEN_SYMBOL,
    TOKEN_CURRENCY,
    TOKEN_DECIMALS,
    deployer.address,   // masterMinter
    deployer.address,   // pauser
    deployer.address,   // blacklister
    deployer.address    // owner
  );
  await fiatToken.waitForDeployment();
  const tokenAddr = await fiatToken.getAddress();
  console.log(`     FiatToken:         ${tokenAddr}`);

  // ─── 4. FiatManager(proxy).initialize ──────────────────────────────
  // Proxy가 모든 호출을 impl로 delegate하므로,
  // impl ABI를 proxy 주소에 attach해서 initialize 호출
  console.log("4/5  FiatManager(proxy).initialize 호출 중...");
  const proxyAsManager = FiatManager.attach(proxyAddr);
  const initTx = await proxyAsManager.initialize(
    tokenAddr,
    deployer.address    // admin = 배포 계정 (gateway 서버 private key의 주소)
  );
  await initTx.wait();
  console.log(`     initialize 완료 (admin: ${deployer.address})`);

  // ─── 5. FiatToken에 FiatManagerProxy를 minter로 등록 ───────────────
  console.log("5/5  FiatToken.configureMinter(proxy) 호출 중...");
  const configureTx = await fiatToken.configureMinter(proxyAddr, MAX_MINT);
  await configureTx.wait();
  console.log(`     minter 등록 완료 (allowance: MaxUint256)`);

  // ─── 배포 결과 출력 (setup.sh가 파싱함) ────────────────────────────
  console.log("\n========== 배포 완료 ==========");
  console.log(`FIAT_MANAGER_IMPL=${implAddr}`);
  console.log(`FIAT_MANAGER_PROXY=${proxyAddr}`);
  console.log(`FIAT_TOKEN=${tokenAddr}`);
  console.log("================================");
}

main()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error(err);
    process.exit(1);
  });
