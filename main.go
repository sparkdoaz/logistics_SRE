package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "fmt"
	_ "log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

var ctx = context.Background()

func main() {
	postgresHost := os.Getenv("POSTGRES_HOST")
	if postgresHost == "" {
		postgresHost = "database-1.cqh017bzo4xj.ap-northeast-1.rds.amazonaws.com"
	}

	redisHost := os.Getenv("REDIS_HOST")

	if redisHost == "" {
		redisHost = "10.0.201.44"
	}

	// 初始化 Gin 路由
	gin.SetMode(gin.DebugMode)
	r := gin.Default()

	// 資料庫連接字符串
	// connStr := "postgres://pqgotest:password@localhost/pqgotest?sslmode=verify-full"
	db, err := sql.Open("postgres", "user=postgres dbname=postgres password=postgres host="+postgresHost+" port=5432  sslmode=disable")
	if err != nil {
		// panic(err)
	} else {
		fmt.Println("postgres connection is established")
	}

	defer db.Close()

	// 创建 Redis 客户端连接
	client := redis.NewClient(&redis.Options{
		Addr:     redisHost + ":6379",
		Password: "", // no password set
		DB:       0,  // use default DB
	})

	// 建立连接
	str, err := client.Ping(ctx).Result()
	if err != nil {
		// panic(err)
	} else {
		fmt.Println("redis connection is established")
	}
	fmt.Print(str)

	// 設定查詢物流數據的端點
	r.GET("/query", queryLogisticsHandler(db, client))

	r.GET("/hi", hi())
	// 啟動 Web 伺服器
	if err := r.Run(":3000"); err != nil {
		panic(err)
	}
}

func hi() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "success",
		})
	}
}

// queryLogisticsHandler 是處理查詢物流數據的處理程序
func queryLogisticsHandler(db *sql.DB, redis *redis.Client) gin.HandlerFunc {
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

		// details, err := get(redis, db, sno, ctx)

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

	// 查询基本信息
	query := `SELECT p.sno, p.tracking_status, p.estimated_delivery, l.title AS location_title
			  FROM Packages p
			  LEFT JOIN TrackingDetails td ON p.sno = td.sno
			  LEFT JOIN Locations l ON td.location_id = l.location_id
			  WHERE p.sno = $1
			  ORDER BY td.date DESC, td.time DESC
			  LIMIT 1`

	row := db.QueryRow(query, sno)
	if err := row.Scan(&pd.Sno, &pd.TrackingStatus, &pd.EstimatedDelivery, &pd.CurrentLocation.Title); err != nil {
		return pd, err
	}

	// 查询追踪细节
	detailsQuery := `SELECT id, date, time, status, location_id
					FROM TrackingDetails
					WHERE sno = $1
					ORDER BY date, time`

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

	// 查询收件人信息
	recipientQuery := `SELECT id, name, address, phone
					  FROM Recipients
					  WHERE sno = $1`

	recipientRow := db.QueryRow(recipientQuery, sno)
	if err := recipientRow.Scan(&pd.Recipient.ID, &pd.Recipient.Name, &pd.Recipient.Address, &pd.Recipient.Phone); err != nil {
		return pd, err
	}

	// 如果查询结果为空，则设置字段为null值
	if pd.Sno == "" {
		return PackageDetails{}, nil
	}

	return pd, nil
}

func get(client *redis.Client, db *sql.DB, sno string, ctx context.Context) (PackageDetails, error) {
	cacheResult, err := client.HGet(ctx, "logistics_cache", sno).Result()

	if err == redis.Nil {
		// 缓存不存在，从数据库获取物流信息
		fmt.Println("cache 沒有資料, 去DB拿")
		packageDetails, _ := getPackageDetailsInDB(sno, db)

		// 将物流信息存入缓存
		if err := setLogisticsInfoInCache(client, ctx, sno, packageDetails); err != nil {
			fmt.Println("Error setting cache:", err)
		}

		// 返回物流信息给用户
		return packageDetails, nil
	} else if err != nil {
		// 出现错误
		fmt.Println("Error:")
		return PackageDetails{}, err
	} else {
		// 缓存命中，直接返回缓存的物流信息
		var packageDetails PackageDetails
		if err := json.Unmarshal([]byte(cacheResult), &packageDetails); err != nil {
			fmt.Println("解析失敗")
			return PackageDetails{}, err
		}
		return packageDetails, nil
	}
}

// 设置物流信息到缓存
func setLogisticsInfoInCache(client *redis.Client, ctx context.Context, sno string, packageDetails PackageDetails) error {
	// 将物流信息转换为 JSON 格式
	data, err := json.Marshal(packageDetails)
	if err != nil {
		fmt.Println("set cache failed:", err)
		return err
	}
	// 设置缓存并指定过期时间（根据业务需求设置）
	client.HSet(ctx, "logistics_cache", sno, data).Err()
	return client.Expire(ctx, "logistics_cache", time.Hour * 2 / 60).Err()
}
