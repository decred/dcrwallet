FROM golang:1.12-stretch

RUN apt-get update && apt-get install -y protobuf-compiler
RUN go get -u github.com/golang/protobuf/protoc-gen-go

WORKDIR /build
CMD "./regen.sh"


