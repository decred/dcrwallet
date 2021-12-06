#!/bin/sh

set -ex

GO=${GO:-go}

${GO} version

${GO} test -short -vet=all "$@" ./...
