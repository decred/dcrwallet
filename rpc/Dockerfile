FROM golang:1.21.0-bookworm

RUN apt-get update && apt-get install -y protobuf-compiler

WORKDIR /build/rpc
CMD ["/bin/bash", "regen.sh"]


