package main

import (
	"fmt"
	"log"

	"selfbet/backend/internal/api"
	"selfbet/backend/internal/config"
	"selfbet/backend/internal/model"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	db, err := model.Open(cfg.DbDriver, cfg.DbDsn, config.DataDir())
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	r := api.NewRouter(cfg, db)
	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	fmt.Printf("SelfBet 运行中: http://localhost:%d  (局域网用本机 IP 访问)\n", cfg.Port)
	if err := r.Run(addr); err != nil {
		log.Fatalf("启动失败: %v", err)
	}
}
