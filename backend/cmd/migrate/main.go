// migrate copies ALL rows from the legacy SQLite file into MySQL, preserving
// primary keys, then verifies row counts and balance sums on both sides.
// Usage: go run ./cmd/migrate -sqlite <path.db> -dsn "root:pass@tcp(127.0.0.1:3306)/" -name selfbet -password <loginPwd>
package main

import (
	"flag"
	"strings"
	"fmt"
	"os"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"selfbet/backend/internal/model"
)

func main() {
	srcDriver := flag.String("src-driver", "sqlite", "源类型：sqlite | mysql")
	sqlitePath := flag.String("sqlite", "", "源 SQLite 文件路径（src-driver=sqlite 时用）")
	srcDsn := flag.String("src-dsn", "", "源 MySQL DSN（含库名，src-driver=mysql 时用），如 root:123456@tcp(127.0.0.1:3306)/selfbet")
	adminDsn := flag.String("dsn", "", "目标 MySQL 管理 DSN（不含库名），如 root:123456@tcp(127.0.0.1:3306)/")
	dbName := flag.String("name", "selfbet", "目标数据库名")
	loginPwd := flag.String("password", "kaytodo", "写入 settings 表的登录密码（目标库无 settings 行时才写入）")
	flag.Parse()
	if (*srcDriver == "sqlite" && *sqlitePath == "") || (*srcDriver == "mysql" && *srcDsn == "") || *adminDsn == "" {
		fmt.Println("用法: migrate -src-driver sqlite|mysql -sqlite <db> -src-dsn <mysql源dsn> -dsn <目标管理dsn> [-name selfbet] [-password kaytodo]")
		os.Exit(1)
	}

	// ---- 源 ----
	var src *gorm.DB
	var err error
	if *srcDriver == "mysql" {
		srcConn := *srcDsn
		if !strings.Contains(srcConn, "parseTime") { // 时间列必须解析为 time.Time
			sep := "?"
			if strings.Contains(srcConn, "?") {
				sep = "&"
			}
			srcConn += sep + "parseTime=true&loc=Local"
		}
		src, err = gorm.Open(mysql.Open(srcConn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	} else {
		src, err = gorm.Open(sqlite.Open(*sqlitePath), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	}
	must(err, "打开源数据库")

	// ---- 目标：MySQL（先建库）----
	admin, err := gorm.Open(mysql.Open(*adminDsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(err, "连接 MySQL")
	must(admin.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci", *dbName)).Error, "建库")
	sqlDB, _ := admin.DB()
	sqlDB.Close()

	dst, err := gorm.Open(mysql.Open(*adminDsn+*dbName+"?charset=utf8mb4&parseTime=true&loc=Local"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(err, "连接目标库")
	must(dst.AutoMigrate(&model.Tag{}, &model.Box{}, &model.ShopItem{}, &model.Task{}, &model.Ledger{}, &model.CashFlow{}, &model.BackpackItem{}, &model.PendingEffect{}, &model.FarmState{}, &model.FarmPlot{}, &model.Settings{}), "AutoMigrate")

	// ---- 复制（清空目标 → 按序全量插入，保留主键）----
	type copier struct {
		name string
		src  interface{}
		dst  interface{}
	}
	run := func(c copier) {
		must(src.Find(c.src).Error, "读取 "+c.name)
		dst.Exec("SET FOREIGN_KEY_CHECKS=0")
		defer dst.Exec("SET FOREIGN_KEY_CHECKS=1")
		must(dst.Where("1 = 1").Delete(c.dst).Error, "清空 "+c.name)
		must(dst.CreateInBatches(c.src, 100).Error, "写入 "+c.name)
	}

	run(copier{"tags", &[]model.Tag{}, &model.Tag{}})
	run(copier{"boxes", &[]model.Box{}, &model.Box{}})
	run(copier{"shop_items", &[]model.ShopItem{}, &model.ShopItem{}})
	run(copier{"tasks", &[]model.Task{}, &model.Task{}})
	run(copier{"ledgers", &[]model.Ledger{}, &model.Ledger{}})
	run(copier{"cash_flows", &[]model.CashFlow{}, &model.CashFlow{}})
	run(copier{"backpack_items", &[]model.BackpackItem{}, &model.BackpackItem{}})
	run(copier{"pending_effects", &[]model.PendingEffect{}, &model.PendingEffect{}})
	run(copier{"farm_states", &[]model.FarmState{}, &model.FarmState{}})
	run(copier{"farm_plots", &[]model.FarmPlot{}, &model.FarmPlot{}})
	run(copier{"settings", &[]model.Settings{}, &model.Settings{}})

	// settings 种子：登录密码
	var sCount int64
	dst.Model(&model.Settings{}).Where("setting_key = ?", model.AuthPasswordKey).Count(&sCount)
	if sCount == 0 {
		must(dst.Create(&model.Settings{SettingKey: model.AuthPasswordKey, Value: *loginPwd}).Error, "seed auth_password")
	}

	// ---- 校验：逐表行数 + 余额 ----
	type check struct {
		name string
		src  interface{}
		dst  interface{}
	}
	checks := []check{
		{"tags", &[]model.Tag{}, &model.Tag{}},
		{"boxes", &[]model.Box{}, &model.Box{}},
		{"shop_items", &[]model.ShopItem{}, &model.ShopItem{}},
		{"tasks", &[]model.Task{}, &model.Task{}},
		{"ledgers", &[]model.Ledger{}, &model.Ledger{}},
		{"cash_flows", &[]model.CashFlow{}, &model.CashFlow{}},
		{"backpack_items", &[]model.BackpackItem{}, &model.BackpackItem{}},
		{"pending_effects", &[]model.PendingEffect{}, &model.PendingEffect{}},
		{"farm_states", &[]model.FarmState{}, &model.FarmState{}},
		{"farm_plots", &[]model.FarmPlot{}, &model.FarmPlot{}},
		{"settings", &[]model.Settings{}, &model.Settings{}},
	}
	allOK := true
	for _, ck := range checks {
		var sn, dn int64
		src.Model(ck.src).Count(&sn)
		dst.Model(ck.dst).Count(&dn)
		ok := sn == dn
		if !ok {
			allOK = false
		}
		fmt.Printf("%-16s sqlite=%d mysql=%d %s\n", ck.name, sn, dn, okMark(ok))
	}
	var sb, dbb int
	src.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS amount").Scan(&sb)
	dst.Model(&model.Ledger{}).Select("COALESCE(SUM(amount),0) AS amount").Scan(&dbb)
	fmt.Printf("余额校验         sqlite=%d mysql=%d %s\n", sb, dbb, okMark(sb == dbb))
	if sb != dbb {
		allOK = false
	}
	fmt.Println("======")
	if allOK {
		fmt.Println("迁移完成：全部校验通过 ✓")
	} else {
		fmt.Println("迁移存在差异 ✗ 请检查上方输出")
		os.Exit(1)
	}
}

func okMark(ok bool) string {
	if ok {
		return "✓"
	}
	return "❌"
}

func must(err error, what string) {
	if err != nil {
		fmt.Printf("[FAIL] %s: %v\n", what, err)
		os.Exit(1)
	}
}
