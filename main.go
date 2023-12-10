package main

import (
	"database/sql"

	_ "fmt"
	_ "log"
	"net/http"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

func main() {
	// 初始化 Gin 路由
	r := gin.Default()

	// 資料庫連接字符串
	// connStr := "postgres://pqgotest:password@localhost/pqgotest?sslmode=verify-full"
	db, err := sql.Open("postgres", "user=cfh00895351 dbname=cfh00895351 sslmode=disable")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// 設定查詢物流數據的端點
	r.GET("/query", queryLogisticsHandler(db))

	// 啟動 Web 伺服器
	if err := r.Run(":8080"); err != nil {
		panic(err)
	}
}

// queryLogisticsHandler 是處理查詢物流數據的處理程序
func queryLogisticsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sno := c.DefaultQuery("sno", "")
		if sno == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sno parameter is required"})
			return
		}

		var id int
		var description, status string

		// 檢查是否存在符合 "sno" 的數據
		err := db.QueryRow("SELECT sno, description, status FROM logistics WHERE sno = $1", sno).Scan(&sno, &description, &status)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "No data found for sno: " + sno})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"id":          id,
			"description": description,
			"status":      status,
		})
	}
}
