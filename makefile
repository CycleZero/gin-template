BINARY_NAME := app
BINARY_PATH := ./bin/${BINARY_NAME}
MAIN_FILE_DIR := ./cmd/main

# protobuf 代码生成（配置结构定义 internal/conf/conf.proto）
# 需安装 buf 与 protoc-gen-go：
#   go install github.com/bufbuild/buf/cmd/buf@latest
#   go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
config:
	buf lint
	buf generate

# wire 依赖注入代码生成（go run 使用 go.mod 锁定的 wire 版本，无需预装 wire 二进制）
wire:
	go run -mod=mod github.com/google/wire/cmd/wire ${MAIN_FILE_DIR}

# 编译
build:
	go build -o ${BINARY_PATH} ${MAIN_FILE_DIR}

# 一键重新构建（config + wire + build）
rebuild: config wire build

# 编译至 Linux AMD64 平台
build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ${BINARY_PATH} ${MAIN_FILE_DIR}

# 运行
run:
	go run ${MAIN_FILE_DIR}

# 安装依赖
tidy:
	go mod tidy
