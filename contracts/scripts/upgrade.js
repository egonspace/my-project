const { ethers } = require("hardhat");

// ────────────────────────────────────────────────────────────────────────────
// upgrade.js: FiatManager 구현체를 새로 배포하고 Proxy를 업그레이드
//
// 사용 방법:
//   DEPLOY_PRIVATE_KEY=<admin-key> \
//   DEPLOY_RPC_URL=<url>           \
//   FIAT_MANAGER_PROXY=<proxy-addr> \
//   npx hardhat run scripts/upgrade.js --network stablenet
// ────────────────────────────────────────────────────────────────────────────

async function main() {
  const proxyAddr = process.env.FIAT_MANAGER_PROXY;
  if (!proxyAddr) {
    throw new Error("FIAT_MANAGER_PROXY 환경변수가 필요합니다.");
  }

  const [deployer] = await ethers.getSigners();
  console.log(`업그레이드 계정: ${deployer.address}`);

  // 1. 새 FiatManager 구현체 배포
  console.log("\n1/2  새 FiatManager(impl) 배포 중...");
  const FiatManager = await ethers.getContractFactory("FiatManager");
  const newImpl = await FiatManager.deploy();
  await newImpl.waitForDeployment();
  const newImplAddr = await newImpl.getAddress();
  console.log(`     새 FiatManager(impl): ${newImplAddr}`);

  // 2. Proxy.upgradeTo(newImpl) 호출
  // FiatManagerProxy는 upgradeTo를 onlyOwner로 제한하므로 deployer가 owner여야 함
  console.log("\n2/2  Proxy 업그레이드 중...");
  const proxyAsManager = FiatManager.attach(proxyAddr);
  const tx = await proxyAsManager.upgradeTo(newImplAddr);
  await tx.wait();
  console.log(`     업그레이드 완료 (proxy: ${proxyAddr} → impl: ${newImplAddr})`);

  console.log("\n========== 업그레이드 완료 ==========");
  console.log(`FIAT_MANAGER_IMPL=${newImplAddr}`);
  console.log(`FIAT_MANAGER_PROXY=${proxyAddr}`);
  console.log("=====================================");
}

main()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error(err);
    process.exit(1);
  });
