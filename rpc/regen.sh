#!/bin/sh

build-protoc-gen-go() {
    mkdir -p bin
    export GOBIN=$PWD/bin
    GO111MODULE=on go install github.com/golang/protobuf/protoc-gen-go
}

generate() {
    protoc -I. api.proto --go_out=plugins=grpc:walletrpc

    # fix uid mapping on files created within the container
    [ -n "$UID" ] && chown -R $UID . 2>/dev/null
}

(cd tools && build-protoc-gen-go)
PATH=$PWD/tools/bin:$PATH generate
