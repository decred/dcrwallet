#!/bin/sh

# setup paths
SCRIPT=$(basename $0)
SCRIPT_DIR=$(dirname $0)
CERTFILE=$SCRIPT_DIR/company0-sign.p12
CERTPWFILE=$SCRIPT_DIR/certpw

# verify params
if [ $# -lt 2 ]; then
	echo "usage: $SCRIPT installer-name installer-dir"
	exit 1
fi

INSTALLER_NAME=$1
shift

if [ -e "$CERTFILE" ]; then
	if [ -e "$CERTPWFILE" ]; then
		read -s PASS < "$CERTPWFILE"
	else
		read -s -p "cert pass: " PASS
	fi
	"$SCRIPT_DIR/signtool" sign //t http://timestamp.digicert.com //f "$CERTFILE" //d "$INSTALLER_NAME" //p $PASS "$@"
fi
