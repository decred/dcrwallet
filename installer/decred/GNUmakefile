# only 3 digits are used, and must increment for upgrades to work
VERS=0.6.0
VERNAME=$(VERS)-beta
INSTALLERNAME="Decred Installer"

all: msi msi64 cleanobjs

current: msi msi64 cleanobjs

TOOLSDIR=../tools

# Wix executables
WIXDIR=$(WIX)bin
HEAT="$(WIXDIR)/heat.exe"
CANDLE="$(WIXDIR)/candle.exe"
LIT="$(WIXDIR)/lit.exe"
LIGHT="$(WIXDIR)/light.exe"

update:

stage:

msi: decred.wxs stage
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" -o decred_x86.wixobj \
	    decred.wxs
	$(LIGHT) -nologo -o decred_$(VERNAME)_x86.msi decred_x86.wixobj \
	    ../dcrd/*_x86.wixlib ../dcrticketbuyer/*_x86.wixlib ../paymetheus/*_x86.wixlib \
	     -ext WixUIExtension -ext WixUtilExtension -b . -b ../resource \
	     -b ../dcrd -b ../dcrticketbuyer -b ../paymetheus
	sh "$(TOOLSDIR)/sign.sh" $(INSTALLERNAME) "$(CURDIR)/*.msi"

msi64: decred.wxs stage
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" -o decred_x64.wixobj \
	    decred.wxs
	$(LIGHT) -nologo -o decred_$(VERNAME)_x64.msi decred_x64.wixobj \
	    ../dcrd/*_x64.wixlib ../dcrticketbuyer/*_x64.wixlib ../paymetheus/*_x64.wixlib \
	     -ext WixUIExtension -ext WixUtilExtension -b . -b ../resource \
	     -b ../dcrd -b ../dcrticketbuyer -b ../paymetheus
	sh "$(TOOLSDIR)/sign.sh" $(INSTALLERNAME) "$(CURDIR)/*.msi"

cleanobjs:
	rm -f decred*.wixobj decred*.wixpdb dcr*.wixlib

clean: cleanobjs
	rm -f decred*.msi

.PHONY: all current msi msi64
