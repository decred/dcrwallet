FROM golang:1.14-buster

RUN apt-get update && apt-get install -y protobuf-compiler

WORKDIR /build/rpc
CMD ["/bin/bash", "regen.sh"]


