# This Makefile is meant to be used by people that do not usually work
# with Go source code. If you know what GOPATH is then you probably
# don't need to bother with make.

.PHONY: aoa android ios aoa-cross swarm evm all test clean
.PHONY: aoa-linux aoa-linux-386 aoa-linux-amd64 aoa-linux-mips64 aoa-linux-mips64le
.PHONY: aoa-linux-arm aoa-linux-arm-5 aoa-linux-arm-6 aoa-linux-arm-7 aoa-linux-arm64
.PHONY: aoa-darwin aoa-darwin-386 aoa-darwin-amd64
.PHONY: aoa-windows aoa-windows-386 aoa-windows-amd64

GOBIN = $(shell pwd)/build/bin
GO ?= latest

aoa:
	build/env.sh go run build/ci.go install ./cmd/aoa
	@echo "Done building."
	@echo "Run \"$(GOBIN)/aoa\" to launch aoa."

test: all
	build/env.sh go run build/ci.go test

clean:
	rm -fr build/_workspace/pkg/ $(GOBIN)/*

# The devtools target installs tools required for 'go generate'.
# You need to put $GOBIN (or $GOPATH/bin) in your PATH to use 'go generate'.

devtools:
	env GOBIN= go get -u golang.org/x/tools/cmd/stringer
	env GOBIN= go get -u github.com/kevinburke/go-bindata/go-bindata
	env GOBIN= go get -u github.com/fjl/gencodec
	env GOBIN= go get -u github.com/golang/protobuf/protoc-gen-go
	env GOBIN= go install ./cmd/abigen
	@type "npm" 2> /dev/null || echo 'Please install node.js and npm'
	@type "solc" 2> /dev/null || echo 'Please install solc'
	@type "protoc" 2> /dev/null || echo 'Please install protoc'

# Cross Compilation Targets (xgo)

aoa-cross: aoa-linux aoa-darwin aoa-windows aoa-android aoa-ios
	@echo "Full cross compilation done:"
	@ls -ld $(GOBIN)/aoa-*

aoa-linux: aoa-linux-386 aoa-linux-amd64 aoa-linux-arm aoa-linux-mips64 aoa-linux-mips64le
	@echo "Linux cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-*

aoa-linux-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/386 -v ./cmd/aoa
	@echo "Linux 386 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep 386

aoa-linux-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/amd64 -v ./cmd/aoa
	@echo "Linux amd64 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep amd64

aoa-linux-arm: aoa-linux-arm-5 aoa-linux-arm-6 aoa-linux-arm-7 aoa-linux-arm64
	@echo "Linux ARM cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep arm

aoa-linux-arm-5:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-5 -v ./cmd/aoa
	@echo "Linux ARMv5 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep arm-5

aoa-linux-arm-6:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-6 -v ./cmd/aoa
	@echo "Linux ARMv6 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep arm-6

aoa-linux-arm-7:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm-7 -v ./cmd/aoa
	@echo "Linux ARMv7 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep arm-7

aoa-linux-arm64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/arm64 -v ./cmd/aoa
	@echo "Linux ARM64 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep arm64

aoa-linux-mips:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips --ldflags '-extldflags "-static"' -v ./cmd/aoa
	@echo "Linux MIPS cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep mips

aoa-linux-mipsle:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mipsle --ldflags '-extldflags "-static"' -v ./cmd/aoa
	@echo "Linux MIPSle cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep mipsle

aoa-linux-mips64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64 --ldflags '-extldflags "-static"' -v ./cmd/aoa
	@echo "Linux MIPS64 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep mips64

aoa-linux-mips64le:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=linux/mips64le --ldflags '-extldflags "-static"' -v ./cmd/aoa
	@echo "Linux MIPS64le cross compilation done:"
	@ls -ld $(GOBIN)/aoa-linux-* | grep mips64le

aoa-darwin: aoa-darwin-386 aoa-darwin-amd64
	@echo "Darwin cross compilation done:"
	@ls -ld $(GOBIN)/aoa-darwin-*

aoa-darwin-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/386 -v ./cmd/aoa
	@echo "Darwin 386 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-darwin-* | grep 386

aoa-darwin-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=darwin/amd64 -v ./cmd/aoa
	@echo "Darwin amd64 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-darwin-* | grep amd64

aoa-windows: aoa-windows-386 aoa-windows-amd64
	@echo "Windows cross compilation done:"
	@ls -ld $(GOBIN)/aoa-windows-*

aoa-windows-386:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/386 -v ./cmd/aoa
	@echo "Windows 386 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-windows-* | grep 386

aoa-windows-amd64:
	build/env.sh go run build/ci.go xgo -- --go=$(GO) --targets=windows/amd64 -v ./cmd/aoa
	@echo "Windows amd64 cross compilation done:"
	@ls -ld $(GOBIN)/aoa-windows-* | grep amd64
