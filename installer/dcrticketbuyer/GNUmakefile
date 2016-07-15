# only 3 digits are used, and must increment for upgrades to work
VERS=0.6.0
VERNAME=$(VERS)-beta
INSTALLERNAME="DCRTicketbuyer Installer"

# Path to project and executables
PROJ=github.com/decred/dcrticketbuyer
PROJDIR=$(GOPATH)/src/$(PROJ)
TOOLSDIR=../tools

# Support files
PROJFILES=ticketbuyer-example.conf
PROJFILES+=webui

# Wix executables
WIXDIR=$(WIX)bin
HEAT="$(WIXDIR)/heat.exe"
CANDLE="$(WIXDIR)/candle.exe"
LIT="$(WIXDIR)/lit.exe"
LIGHT="$(WIXDIR)/light.exe"

all: msi cleanobjs

current: msi cleanobjs

stage:
	rm -rf staging
	mkdir staging staging/common staging/x86 staging/x64
	$(foreach PROG,$(PROJFILES),cp -r "$(PROJDIR)/$(PROG)" staging/common/;)
	(cd staging/x86 && GOARCH=386 go build $(PROJ))
	(cd staging/x64 && GOARCH=amd64 go build $(PROJ))
	sh "$(TOOLSDIR)/sign-staging.sh" "$(CURDIR)/staging/x86/*.exe" "$(CURDIR)/staging/x64/*.exe"

harvest: stage
	$(HEAT) dir staging/common -nologo -gg -cg DCRTicketbuyerCommonFiles -sreg -sfrag \
	    -srd -suid -dr DCRTROOT -var var.SourceDir -out staging/dcrtproj.wxs
	$(HEAT) dir staging/x86 -nologo -gg -cg DCRTArchFiles -sreg -sfrag -srd -suid \
	    -dr DCRTROOT -var var.SourceDir -out staging/dcrtarch_x86.wxs
	$(HEAT) dir staging/x64 -nologo -gg -cg DCRTArchFiles -sreg -sfrag -srd -suid \
	    -dr DCRTROOT -var var.SourceDir -out staging/dcrtarch_x64.wxs

wixlib: dcrticketbuyer_common.wxs harvest
#	x86
	$(CANDLE) -nologo -arch x86 -dSourceDir=staging/common -o dcrtproj_x86.wixobj \
	    staging/dcrtproj.wxs
	$(CANDLE) -nologo -arch x86 -dSourceDir=staging/x86 -o dcrtarch_x86.wixobj \
	    staging/dcrtarch_x86.wxs
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" -o dcrtcommon_x86.wixobj \
	    dcrticketbuyer_common.wxs
	$(LIT) -nologo -o dcrticketbuyer_$(VERNAME)_x86.wixlib dcrtcommon_x86.wixobj \
	    dcrtproj_x86.wixobj dcrtarch_x86.wixobj -b .
#	x64
	$(CANDLE) -nologo -arch x64 -dSourceDir=staging/common -o dcrtproj_x64.wixobj \
	    staging/dcrtproj.wxs
	$(CANDLE) -nologo -arch x64 -dSourceDir=staging/x64 -o dcrtarch_x64.wixobj \
	    staging/dcrtarch_x64.wxs
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" -o dcrtcommon_x64.wixobj \
	    dcrticketbuyer_common.wxs
	$(LIT) -nologo -o dcrticketbuyer_$(VERNAME)_x64.wixlib dcrtcommon_x64.wixobj \
	    dcrtproj_x64.wixobj dcrtarch_x64.wixobj -b .


# Intentionally not built as we use wixlibs instead currently, but the logic
# is all there for proper build of merge modules should we ever need to
# provide to a third party.
msm: dcrticketbuyer_msm.wxs wixlib
#	x86
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" -o dcrtmsm_x86.wixobj \
	    dcrticketbuyer_msm.wxs
	$(LIGHT) -nologo -o dcrticketbuyer_$(VERNAME)_x86.msm dcrtmsm_x86.wixobj \
	    dcrticketbuyer_$(VERNAME)_x86.wixlib -b . -b ../resource
#	x64
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" -o dcrtmsm_x64.wixobj \
	    dcrticketbuyer_msm.wxs
	$(LIGHT) -nologo -o dcrticketbuyer_$(VERNAME)_x64.msm dcrtmsm_x64.wixobj \
	    dcrticketbuyer_$(VERNAME)_x64.wixlib -b . -b ../resource

msi: dcrticketbuyer_common.wxs wixlib
#	x86
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" \
	    -o dcrt_standalone_x86.wixobj dcrticketbuyer_standalone.wxs
	$(LIGHT) -nologo -o dcrticketbuyer_standalone_$(VERNAME)_x86.msi \
	    dcrt_standalone_x86.wixobj dcrticketbuyer_$(VERNAME)_x86.wixlib \
	    -ext WixUIExtension -b . -b ../resource
#	x64
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" \
	    -o dcrt_standalone_x64.wixobj dcrticketbuyer_standalone.wxs
	$(LIGHT) -nologo -o dcrticketbuyer_standalone_$(VERNAME)_x64.msi \
	    dcrt_standalone_x64.wixobj dcrticketbuyer_$(VERNAME)_x64.wixlib \
	    -ext WixUIExtension -b . -b ../resource
	sh "$(TOOLSDIR)/sign.sh" $(INSTALLERNAME) "$(CURDIR)/*.msi"


cleanobjs:
	rm -f dcrt*.wixobj dcrt*.wixpdb staging/*.wxs

clean: cleanobjs
	rm -rf staging
	rm -f dcrticketbuyer*.wixlib dcrticketbuyer*.msm dcrticketbuyer*.msi

.PHONY: all current stage harvest wixlib msm msi wixlib64 msm64 msi64 cleanobjs clean
