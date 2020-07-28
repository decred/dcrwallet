#!/bin/sh

set -ex

go version

go test -short -vet=all ./...
