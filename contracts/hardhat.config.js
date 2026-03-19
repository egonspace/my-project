require("@nomicfoundation/hardhat-toolbox");

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  solidity: {
    version: "0.8.24",
    settings: {
      optimizer: {
        enabled: true,
        runs: 200,
      },
    },
  },
  paths: {
    sources: ".",       // .sol 파일이 프로젝트 루트에 위치
    tests: "./test",
    cache: "./cache",
    artifacts: "./artifacts",
  },
  networks: {
    hardhat: {},
    localhost: {
      url: "http://127.0.0.1:8545",
    },
    // setup.sh가 환경변수로 주입: DEPLOY_RPC_URL, DEPLOY_PRIVATE_KEY
    stablenet: {
      url: process.env.DEPLOY_RPC_URL || "",
      accounts: process.env.DEPLOY_PRIVATE_KEY
        ? [process.env.DEPLOY_PRIVATE_KEY]
        : [],
    },
  },
};
