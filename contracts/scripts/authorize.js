const { ethers } = require("hardhat");

// FiatManager.authorize(address) 최소 ABI
const FIAT_MANAGER_ABI = [
  "function authorize(address _user) external",
  "function authorized(address) view returns (bool)",
];

async function main() {
  const proxyAddr  = process.env.FIAT_MANAGER_PROXY;
  const userAddr   = process.env.SAMPLE_ADDRESS;

  if (!proxyAddr || !userAddr) {
    throw new Error("FIAT_MANAGER_PROXY, SAMPLE_ADDRESS 환경변수가 필요합니다.");
  }

  const [admin] = await ethers.getSigners();
  console.log(`authorize 호출 계정: ${admin.address}`);

  const fiatManager = new ethers.Contract(proxyAddr, FIAT_MANAGER_ABI, admin);

  // 이미 authorized면 skip
  const already = await fiatManager.authorized(userAddr);
  if (already) {
    console.log(`AUTHORIZE_SKIPPED=${userAddr} (already authorized)`);
    return;
  }

  const tx = await fiatManager.authorize(userAddr);
  await tx.wait();
  console.log(`AUTHORIZE_OK=${userAddr}`);
}

main()
  .then(() => process.exit(0))
  .catch((err) => {
    console.error(err);
    process.exit(1);
  });
