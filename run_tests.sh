#!/usr/bin/env bash

set -ex

go version

go test -short ./...
