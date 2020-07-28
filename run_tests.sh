#!/usr/bin/env bash

set -ex

go version

go test -vet=all -short ./...
