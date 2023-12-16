# 選擇基礎的 image
FROM golang:latest
# 設置工作目錄
WORKDIR /app
# 複製 Go mod 和 dependence list
COPY go.mod ./
COPY go.sum ./
# 下載 dependence
RUN go mod download
# 複製資料（會根據 .dockerignore 來進行略過）
COPY . .
# 去 Build 程式叫做 main 
RUN go build -o main .
# 提供描述告知 會使用 3000 port
EXPOSE 3000
# 運行程式
CMD ["/app/main"]