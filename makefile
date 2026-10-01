BINARY_NAME := app
BINARY_PATH := ./bin/${BINARY_NAME}
MAIN_FILE_DIR := ./

# wire 依赖注入代码生成（go run 使用 go.mod 锁定的 wire 版本，无需预装 wire 二进制）
wire:
	go run -mod=mod github.com/google/wire/cmd/wire ${MAIN_FILE_DIR}

# 编译
build:
	go build -o ${BINARY_PATH} ${MAIN_FILE_DIR}

# 一键重新构建（wire + build）
rebuild: wire build

# 编译至 Linux AMD64 平台
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ${BINARY_PATH} ${MAIN_FILE_DIR}

# 运行
run:
	go run .

# 安装依赖
tidy:
	go mod tidy
