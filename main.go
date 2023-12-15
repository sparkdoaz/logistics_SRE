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
	gin.SetMode(gin.DebugMode)
	r := gin.Default()

	// 資料庫連接字符串
	// connStr := "postgres://pqgotest:password@localhost/pqgotest?sslmode=verify-full"
	db, err := sql.Open("postgres", "user=postgres dbname=postgres password=postgres host=localhost port=5432  sslmode=disable")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	// 設定查詢物流數據的端點
	r.GET("/query", queryLogisticsHandler(db))

	// 啟動 Web 伺服器
	if err := r.Run("127.0.0.1:3000"); err != nil {
		panic(err)
	}
}

// queryLogisticsHandler 是處理查詢物流數據的處理程序
func queryLogisticsHandler(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		sno := c.DefaultQuery("sno", "")
		if sno == "" {
			c.JSON(404, gin.H{
				"status": "error",
				"data":   nil,
				"error": gin.H{
					"code":    404,
					"message": "Tracking number not found",
				},
			})
			return
		}

		details, err := getPackageDetailsInDB(sno, db)

		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(404, gin.H{
					"status": "error",
					"data":   nil,
					"error": gin.H{
						"code":    404,
						"message": "Tracking number not found",
					},
				})
			} else {
				c.JSON(404, gin.H{
					"status": "error",
					"data":   nil,
					"error": gin.H{
						"code":    404,
						"message": "Tracking number not found",
					},
				})
			}
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status": "success",
			"data":   details,
			"error":  err,
		})
	}
}

func getPackageDetailsInDB(sno string, db *sql.DB) (PackageDetails, error) {
	var pd PackageDetails

	// 查詢基本資訊s
	query := `SELECT sno, tracking_status, estimated_delivery FROM Packages WHERE sno = $1`
	row := db.QueryRow(query, sno)
	if err := row.Scan(&pd.Sno, &pd.TrackingStatus, &pd.EstimatedDelivery); err != nil {
		return pd, err
	}

	// 查詢追蹤細節sss
	detailsQuery := `SELECT id, date, time, status, location_id FROM TrackingDetails WHERE sno = $1`
	rows, err := db.Query(detailsQuery, sno)
	if err != nil {
		return pd, err
	}
	defer rows.Close()

	for rows.Next() {
		var td TrackingDetail

		if err := rows.Scan(&td.ID, &td.Date, &td.Time, &td.Status, &td.LocationID); err != nil {
			return pd, err
		}
		pd.Details = append(pd.Details, td)
	}

	// 查詢收件人資訊
	recipientQuery := `SELECT id, name, address, phone FROM Recipients WHERE sno = $1`
	recipientRow := db.QueryRow(recipientQuery, sno)
	if err := recipientRow.Scan(&pd.Recipient.ID,
		&pd.Recipient.Name, &pd.Recipient.Address, &pd.Recipient.Phone); err != nil {
		return pd, err
	}

	// 查詢當前位置資訊
	locationQuery := `SELECT location_id, title, city, address FROM Locations WHERE location_id = (SELECT location_id FROM TrackingDetails WHERE sno = $1 ORDER BY date DESC, time DESC LIMIT 1)`
	locationRow := db.QueryRow(locationQuery, sno)
	if err := locationRow.Scan(
		&pd.CurrentLocation.LocationID,
		&pd.CurrentLocation.Title,
		&pd.CurrentLocation.City,
		&pd.CurrentLocation.Address,
	); err != nil {
		return pd, err
	}

	return pd, nil
}
