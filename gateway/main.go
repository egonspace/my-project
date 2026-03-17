package main

import (
	"context"
	"gateway/api"
	"gateway/blockchain"
	"gateway/config"
	"gateway/db"
	"gateway/firmbanking"
	"gateway/listener"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cfg := config.Default()

	stateDB, err := db.NewStateDB(cfg.DatabaseDSN)
	if err != nil {
		log.Fatalf("failed to connect to StateDB: %v", err)
	}
	defer stateDB.Close()

	if err := stateDB.CreateTable(); err != nil {
		log.Fatalf("failed to create table: %v", err)
	}
	log.Println("[Main] StateDB connected")

	bcClient := blockchain.NewStubClient(cfg.BlockchainRPCURL, cfg.FiatManagerAddr, cfg.AdminPrivateKey)
	fbClient := firmbanking.NewStubClient(cfg.FirmBankingURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	chainListener := listener.NewChainListener(stateDB, bcClient, fbClient)
	if err := chainListener.Start(ctx); err != nil {
		log.Fatalf("failed to start ChainListener: %v", err)
	}
	log.Println("[Main] ChainListener started")

	server := api.NewServer(stateDB, bcClient)
	go func() {
		log.Printf("[Main] GatewayAPI listening on %s", cfg.ServerPort)
		if err := server.Run(cfg.ServerPort); err != nil {
			log.Fatalf("GatewayAPI server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("[Main] shutting down...")
	cancel()
}
