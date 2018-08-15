#!/bin/sh

set -e

GO=go
if [[ $GOVERSION == 1.10 ]]; then
    GO=vgo
fi

MODULES="chain deployments errors internal/helpers internal/zero lru p2p"
MODULES="${MODULES} pgpwordlist spv ticketbuyer ticketbuyer/v2 validate"
MODULES="${MODULES} version wallet walletseed ."

for module in $MODULES; do
    (cd $module && $GO test -short ./... && $GO mod verify >/dev/null)
done
