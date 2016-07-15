#!/bin/sh

# setup paths
SCRIPT=$(basename $0)
SCRIPT_DIR=$(dirname $0)
CERTFILE=$SCRIPT_DIR/company0-sign.p12
CERTPWFILE=$SCRIPT_DIR/certpw

# verify params
if [ $# -lt 1 ]; then
	echo "usage: $SCRIPT staging-dir"
	exit 1
fi

if [ -e "$CERTFILE" ]; then
	if [ -e "$CERTPWFILE" ]; then
		read -s PASS < "$CERTPWFILE"
	else
		read -s -p "cert pass: " PASS
	fi
	"$SCRIPT_DIR/signtool" sign //t http://timestamp.digicert.com //f "$CERTFILE" //p $PASS "$@"
fi
