package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	api "github.com/hashicorp/consul/api"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	_ "os"

	_ "github.com/hashicorp/vault-client-go/schema"

	vault "github.com/hashicorp/vault/api"
)

var (
	ctx          = context.Background()
	db           *sql.DB
	client       *redis.Client
	consulClient *api.Client
	vaultClient  *vault.Client
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "SPARKHUANG_http_requests_total",
			Help: "Count of all HTTP requests",
		},
		[]string{"path"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "SPARKHUANG_http_request_duration_seconds",
			Help:    "Duration of HTTP requests.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"path"},
	)

	httpResponseStatus = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "SPARKHUANG_http_response_status_total",
			Help: "Counts of response statuses",
		},
		[]string{"status"},
	)
)

func main() {
	InitLogger()
	sugarLogger.Info("Logging from main")

	err := godotenv.Load()
	if err != nil {
		sugarLogger.Info("Error loading env file:", err)
	}

	DBAndRedisInit()

	// 初始化 Consul 客戶端
	config := api.DefaultConfig()
	config.Address = NewConsulConnectAddress()
	temp, err := api.NewClient(config)
	if err != nil {
		sugarLogger.Info("Failed to create Consul client: %v", err)
	} else {
		consulClient = temp
	}

	// 从环境变量获取 Vault 地址和凭证信息
	// vaultAddr := os.Getenv("VAULT_ADDR")
	// vaultToken := os.Getenv("VAULT_TOKEN")

	// 创建 Vault 客户端配置
	vaultConfig := vault.DefaultConfig()
	vaultConfig.Address = NewVaultConnectAddress()

	// 初始化 Vault 客戶端
	temp2, err := vault.NewClient(vaultConfig)
	if err != nil {
		sugarLogger.Info("Failed to create Vault client: %v", err)
	} else {
		vaultClient = temp2
	}
	// 读取 Kubernetes 服务账户的 Token
	jwtToken, useKubernetesAuth := readKubernetesToken()
	vaultClient.SetToken(jwtToken)

	if useKubernetesAuth {
		// 使用 Kubernetes 认证
		err := kubernetesLogin(jwtToken)
		if err != nil {
			fmt.Printf("Kubernetes auth failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Kubernetes auth successful")
	} else {
		fmt.Println("Using default or environment-specified token for Vault authentication")
	}

	// 如果使用 Kubernetes 认证，则进行 Role 验证

	// 初始化 Gin 路由
	gin.SetMode(gin.DebugMode)
	r := gin.Default()

	// # 普魯米修斯區塊
	// 添加 普魯米修斯 Prometheus metrics中间件
	r.Use(prometheusMiddleware())
	// 普魯米修斯區塊
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// 最簡單的 Health Check
	r.GET("/", gin.HandlerFunc(func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	}))

	// # Vault 區塊
	// Endpoint to get value from Vault
	r.GET("/vault-kv", getVaultKVHandler)

	// Endpoint to set value to Vault
	r.PUT("/vault-kv-set", setVaultKVHandler)

	// # Consul 區塊
	// GET 方法用于檢查 Consul 連線
	r.GET("/check-consul", checkConsulConnection)
	// GET 方法用于从 Consul 获取指定键的值
	r.GET("/consul-kv", getConsulValueHandler)
	// PUT 方法用于将指定值设置到 Consul 的指定键
	r.PUT("/consul-kv", putConsulValueHandler)

	r.GET("/healthcheck", HealthCheckHandler(db, client))

	r.GET("/radom", gin.HandlerFunc(func(c *gin.Context) {
		currentTime := time.Now()
		timeStr := currentTime.Format("2006-01-02_15-04-05")
		c.JSON(200, gin.H{
			"random": timeStr,
		})
	}))

	// 設定查詢物流數據的端點
	r.GET("/query", queryLogisticsHandler(db, client))

	// 啟動 Web 伺服器
	if err := r.Run(":3000"); err != nil {
		sugarLogger.Fatalf("無法啟動 Web 伺服器:%v", err)
	}
}

// 普羅米修斯區塊
// prometheusMiddleware 返回Prometheus metrics中间件
func prometheusMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		// 继续处理请求
		c.Next()

		duration := time.Since(start).Seconds()

		// 记录请求的信息
		path := c.FullPath()
		status := c.Writer.Status()

		// 增加http_requests_total计数器
		httpRequestsTotal.WithLabelValues(path).Inc()

		// 记录请求持续时间
		httpRequestDuration.WithLabelValues(path).Observe(duration)

		// 增加http_response_status_total计数器
		httpResponseStatus.WithLabelValues(http.StatusText(status)).Inc()

		sugarLogger.Info("Request Path: %s, Status: %d, Duration: %.3f seconds\n", path, status, duration)
	}
}

// Consul 的邏輯區塊

func checkConsulConnection(c *gin.Context) {
	if consulClient == nil {
		c.JSON(500, gin.H{
			"status": "error",
			"error":  "Consul client is not initialized",
		})
	} else {
		c.JSON(200, gin.H{
			"status": "ok",
		})
	}
}

// 处理获取 Consul 键值对的请求
func getConsulValueHandler(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Key query parameter is required"})
		return
	}

	value, err := getConsulValue(key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to retrieve key from Consul: %s", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"key": key, "value": value})
}

// 处理设置 Consul 键值对的请求
func putConsulValueHandler(c *gin.Context) {
	key := c.Query("key")
	value := c.Query("value")
	if key == "" || value == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both key and value query parameters are required"})
		return
	}

	err := putConsulValue(key, value)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to set key in Consul: %s", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("Key '%s' set to '%s' in Consul", key, value)})
}

// 从 Consul 获取指定键的值
func getConsulValue(key string) (string, error) {
	kv := consulClient.KV()
	pair, _, err := kv.Get(key, nil)
	if err != nil {
		return "", err
	}
	if pair == nil {
		return "", fmt.Errorf("key '%s' not found in Consul", key)
	}
	return string(pair.Value), nil
}

// 将指定值设置到 Consul 的指定键
func putConsulValue(key, value string) error {
	kv := consulClient.KV()
	p := &api.KVPair{Key: key, Value: []byte(value)}
	_, err := kv.Put(p, nil)
	return err
}

// ////// 物流邏輯區域 ////////
// queryLogisticsHandler 是處理查詢物流數據的處理程序
func queryLogisticsHandler(db *sql.DB, redis *redis.Client) gin.HandlerFunc {
	if db == nil {
		return HealthCheckHandler(db, client)
	} else {
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

			details, err := get(redis, db, sno, ctx)

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
}

func getPackageDetailsInDB(sno string, db *sql.DB) (PackageDetails, error) {
	var pd PackageDetails

	// 查詢基本資訊s
	query := `SELECT sno, tracking_status, estimated_delivery FROM Packages WHERE sno = $1`
	row := db.QueryRow(query, sno)
	if err := row.Scan(&pd.Sno, &pd.TrackingStatus, &pd.EstimatedDelivery); err != nil {
		fmt.Println("	if err := row.Scan(&pd.Sno, &pd.TrackingStatus, &pd.EstimatedDelivery); err != nil {")
		return pd, err
	}

	// 查詢追蹤細節
	detailsQuery := `SELECT id, date, time, status, location_id FROM TrackingDetails WHERE sno = $1`
	rows, err := db.Query(detailsQuery, sno)
	if err != nil {
		fmt.Println("// 查詢追蹤細節 error:", err)
		return pd, err
	}
	defer rows.Close()

	for rows.Next() {
		var td TrackingDetail

		if err := rows.Scan(&td.ID, &td.Date, &td.Time, &td.Status, &td.LocationID); err != nil {
			fmt.Println("		if err := rows.Scan(&td.ID, &td.Date, &td.Time, &td.Status, &td.LocationID); err != nil {", err)
			return pd, err
		}
		pd.Details = append(pd.Details, td)
	}

	// 查詢收件人資訊
	recipientQuery := `SELECT id, name, address, phone FROM Recipients WHERE sno = $1`
	recipientRow := db.QueryRow(recipientQuery, sno)
	if err := recipientRow.Scan(&pd.Recipient.ID,
		&pd.Recipient.Name, &pd.Recipient.Address, &pd.Recipient.Phone); err != nil {
		fmt.Println("recipientQuery", err)
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
		fmt.Println("這裡有 error")
		return pd, err
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
	return client.Expire(ctx, "logistics_cache", time.Hour*2/60).Err()
}

func getVaultKVHandler(c *gin.Context) {
	key := c.Query("key")
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Key query parameter is required"})
		return
	}

	// Read from Vault
	result, err := vaultClient.Logical().Read(fmt.Sprintf("secret/data/%s", key))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve key from Vault"})
		return
	}

	if result == nil || result.Data == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("Key '%s' not found", key)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"key": key, "value": result.Data["data"]})
}

func setVaultKVHandler(c *gin.Context) {
	key := c.Query("key")
	value := c.Query("value")
	if key == "" || value == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Both key and value query parameters are required"})
		return
	}

	// Write to Vault
	path := fmt.Sprintf("secret/data/%s", key)
	dataToWrite := map[string]interface{}{"data": value}
	_, err := vaultClient.Logical().Write(path, dataToWrite)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set key in Vault"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("Key '%s' set to '%s'", key, value)})
}

// 讀取 VAR
func readKubernetesToken() (string, bool) {
	var jwtToken string
	var useKubernetesAuth bool

	// 尝试读取 Kubernetes 服务账户的 Token
	tokenPath := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	token, err := os.ReadFile(tokenPath)
	if err == nil {
		jwtToken = string(token)
		fmt.Printf("Get jwtToken: %s\n", jwtToken)
		useKubernetesAuth = true
	} else {
		fmt.Println("Service account token not found, using environment variable or default token")
		jwtToken = os.Getenv("VAULT_TOKEN")
		if jwtToken == "" {
			jwtToken = "hvs.4uMnbbSl6VyoNhCbbBgkluuq" // 默认的 Vault Token
		}
		useKubernetesAuth = false
	}

	return jwtToken, useKubernetesAuth
}

func kubernetesLogin(jwtToken string) error {
	vaultPath := os.Getenv("K8S_PATH")
	if vaultPath == "" {
		vaultPath = "example-cluster"
	}
	role := os.Getenv("VAULT_ROLE")
	if role == "" {
		role = "example-role"
	}

	// 使用 Kubernetes 认证进行 Role 验证
	_, err := vaultClient.Logical().Write(fmt.Sprintf("auth/kubernetes/login/%s", vaultPath), map[string]interface{}{
		"role": role,
		"jwt":  jwtToken,
	})
	if err != nil {
		return err
	}
	return nil
}
