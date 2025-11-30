.PHONY: run build test
build:
	@echo "正在进行编辑"
	go build -o ./bin/main main.go
test:
	@echo "运行测试"
	go test ./...
run: build
	@echo "正在执行"
	./bin/main
push:
	git add .
	git commit -m "$m"
	git push 