package main

import (
	"fmt"
	"os"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"selfbet/backend/internal/model"
)

// 临时脚本：还原误触发的 A1 升级（删流水行 + level_a1 归零）
func main() {
	dsn := "root:kaytodo666@tcp(121.196.247.87:3306)/selfbet?charset=utf8mb4&parseTime=True&loc=Asia%2FShanghai"
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		fmt.Println("连接失败:", err)
		os.Exit(1)
	}
	var rows []model.Ledger
	db.Where("type = ? AND amount = ? AND note LIKE ?", "farm", -50, "%产量A1（拓展）%").Order("id DESC").Find(&rows)
	if len(rows) == 0 {
		fmt.Println("未找到该流水行")
		return
	}
	row := rows[0]
	fmt.Printf("删除流水行 id=%d note=%s\n", row.ID, row.Note)
	db.Delete(&model.Ledger{}, row.ID)
	db.Model(&model.FarmState{}).Where("id = 1").Update("level_a1", 0)
	var s model.FarmState
	db.Take(&s)
	fmt.Printf("FarmState 还原: level_a1=%d\n", s.LevelA1)
	var bal int64
	db.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS b").Scan(&bal)
	fmt.Printf("余额: %d\n", bal)
}
