#!/usr/bin/env bash
file_name="common"
proto_dir="/home/he/aoa/go/src/github.com/Aurorachain/go-aoa/internal/grpc/utils"
go_src_path="${GOPATH}/src/"
protoc -I ${go_src_path} ${proto_dir}/${file_name}.proto --go_out=plugins=grpc:${go_src_path}