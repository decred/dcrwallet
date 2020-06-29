#!/bin/sh

build_protoc_gen_go() {
    mkdir -p bin
    export GOBIN=$PWD/bin
    GO111MODULE=on go install github.com/golang/protobuf/protoc-gen-go
}

generate() {
    protoc -I. api.proto --go_out=plugins=grpc:walletrpc --go_opt=paths=source_relative

    # fix uid mapping on files created within the container
    [ -n "$UID" ] && chown -R $UID . 2>/dev/null
}

(cd tools && build_protoc_gen_go)
PATH=$PWD/tools/bin:$PATH generate
