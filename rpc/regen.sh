#!/bin/sh

build_tools() {
    mkdir -p bin
    export GOBIN=$PWD/bin
    go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
    go install google.golang.org/protobuf/cmd/protoc-gen-go
}

generate() {
    protoc -I. api.proto --go_out=walletrpc --go-grpc_out=walletrpc \
        --go_opt=paths=source_relative --go-grpc_opt=paths=source_relative

    # fix uid mapping on files created within the container
    [ -n "$UID" ] && chown -R $UID . 2>/dev/null || return 0
}

(cd tools && build_tools)
PATH=$PWD/tools/bin:$PATH generate
