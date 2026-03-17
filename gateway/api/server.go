package api

import (
	"gateway/blockchain"
	"gateway/db"

	"github.com/gin-gonic/gin"
)

type Server struct {
	router  *gin.Engine
	handler *Handler
}

func NewServer(stateDB *db.StateDB, bc blockchain.Client) *Server {
	handler := NewHandler(stateDB, bc)
	router := gin.Default()

	v1 := router.Group("/api/v1")
	{
		v1.POST("/deposit", handler.HandleDeposit)
		v1.POST("/mint/:id/retry", handler.HandleRetryMint)
		v1.GET("/request/:id", handler.HandleGetRequest)
	}

	return &Server{
		router:  router,
		handler: handler,
	}
}

func (s *Server) Run(addr string) error {
	return s.router.Run(addr)
}
