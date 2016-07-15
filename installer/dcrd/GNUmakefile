# only 3 digits are used, and must increment for upgrades to work
VERS=0.6.0
VERNAME=$(VERS)-beta
INSTALLERNAME="Dcrd Installer"

# Path to project and executables
PROJ=github.com/decred/dcrd
PROJDIR=$(GOPATH)/src/$(PROJ)
PROJPROGS=$(PROJ) $(PROJ)/cmd/dcrctl
TOOLSDIR=../tools

# Support files
PROJFILES=sample-dcrd.conf

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
	$(foreach PROG,$(PROJFILES),cp "$(PROJDIR)/$(PROG)" staging/common/;)
	(cd staging/x86 $(foreach PROG,$(PROJPROGS),&& GOARCH=386 go build $(PROG)))
	(cd staging/x64 $(foreach PROG,$(PROJPROGS),&& GOARCH=amd64 go build $(PROG)))
	sh "$(TOOLSDIR)/sign-staging.sh" "$(CURDIR)/staging/x86/*.exe" "$(CURDIR)/staging/x64/*.exe"

harvest: stage
	$(HEAT) dir staging/common -nologo -gg -cg DCRDCommonFiles -sreg -sfrag -srd -suid \
	    -dr DCRDROOT -var var.SourceDir -out staging/dcrdproj.wxs
	$(HEAT) dir staging/x86 -nologo -gg -cg DCRDArchFiles -sreg -sfrag -srd -suid \
	    -dr DCRDROOT -var var.SourceDir -out staging/dcrdarch_x86.wxs
	$(HEAT) dir staging/x64 -nologo -gg -cg DCRDArchFiles -sreg -sfrag -srd -suid \
	    -dr DCRDROOT -var var.SourceDir -out staging/dcrdarch_x64.wxs

wixlib: dcrd_common.wxs harvest
#	x86
	$(CANDLE) -nologo -arch x86 -dSourceDir=staging/common -o dcrdproj_x86.wixobj \
	    staging/dcrdproj.wxs
	$(CANDLE) -nologo -arch x86 -dSourceDir=staging/x86 -o dcrdarch_x86.wixobj \
	    staging/dcrdarch_x86.wxs
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" -o dcrdcommon_x86.wixobj \
	    dcrd_common.wxs
	$(LIT) -nologo -o dcrd_$(VERNAME)_x86.wixlib dcrdcommon_x86.wixobj \
	    dcrdproj_x86.wixobj dcrdarch_x86.wixobj -b .
#	x64
	$(CANDLE) -nologo -arch x64 -dSourceDir=staging/common -o dcrdproj_x64.wixobj \
	    staging/dcrdproj.wxs
	$(CANDLE) -nologo -arch x64 -dSourceDir=staging/x64 -o dcrdarch_x64.wixobj \
	    staging/dcrdarch_x64.wxs
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" -o dcrdcommon_x64.wixobj \
	    dcrd_common.wxs
	$(LIT) -nologo -o dcrd_$(VERNAME)_x64.wixlib dcrdcommon_x64.wixobj \
	    dcrdproj_x64.wixobj dcrdarch_x64.wixobj -b .

# Intentionally not built as we use wixlibs instead currently, but the logic
# is all there for proper build of merge modules should we ever need to
# provide to a third party.
msm: dcrd_msm.wxs wixlib
#	x86
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" -o dcrdmsm_x86.wixobj \
	    dcrd_msm.wxs
	$(LIGHT) -nologo -o dcrd_$(VERNAME)_x86.msm dcrdmsm_x86.wixobj \
	    dcrd_$(VERNAME)_x86.wixlib -b . -b ../resource
#	x64
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" -o dcrdmsm_x64.wixobj \
	    dcrd_msm.wxs
	$(LIGHT) -nologo -o dcrd_$(VERNAME)_x64.msm dcrdmsm_x64.wixobj \
	    dcrd_$(VERNAME)_x64.wixlib -b . -b ../resource

msi: dcrd_standalone.wxs wixlib
#	x86
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" \
	    -o dcrd_standalone_x86.wixobj dcrd_standalone.wxs
	$(LIGHT) -nologo -o dcrd_standalone_$(VERNAME)_x86.msi \
	    dcrd_standalone_x86.wixobj dcrd_$(VERNAME)_x86.wixlib \
	    -ext WixUIExtension -b . -b ../resource
#	x64
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" \
	    -o dcrd_standalone_x64.wixobj dcrd_standalone.wxs
	$(LIGHT) -nologo -o dcrd_standalone_$(VERNAME)_x64.msi \
	    dcrd_standalone_x64.wixobj dcrd_$(VERNAME)_x64.wixlib \
	    -ext WixUIExtension -b . -b ../resource
	sh "$(TOOLSDIR)/sign.sh" $(INSTALLERNAME) "$(CURDIR)/*.msi"

cleanobjs:
	rm -f dcrd*.wixobj dcrd*.wixpdb staging/*.wxs

clean: cleanobjs
	rm -rf staging
	rm -f dcrd*.wixlib dcrd*.msm dcrd*.msi

.PHONY: all current stage harvest wixlib msm msi wixlib64 msm64 msi64 cleanobjs clean
