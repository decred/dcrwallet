# only 3 digits are used, and must increment for upgrades to work
VERS=0.6.0
VERNAME=$(VERS)-beta
INSTALLERNAME="Paymetheus Installer"

# Path to project and executables
TOOLSDIR=../tools

PAYMETHEUS=~/git/Paymetheus
MSBUILD=/c/Program\ Files\ \(x86\)/MSBuild/14.0/Bin/MSbuild.exe
NUGET=~/bin/nuget.exe

# Support files
PROJFILES=Paymetheus/bin/x86/Release/*.exe
PROJFILES+=Paymetheus/bin/x86/Release/*.dll
PROJFILES+=Paymetheus/bin/x86/Release/nativelibs
PROJFILES64=Paymetheus/bin/x64/Release/*.exe
PROJFILES64+=Paymetheus/bin/x64/Release/*.dll
PROJFILES64+=Paymetheus/bin/x64/Release/nativelibs
PROJWALLET=github.com/decred/dcrwallet

# nuget directories
NUGETPKG=Paymetheus
NUGETPKG+=Paymetheus.Decred
NUGETPKG+=Paymetheus.Rpc
NUGETPKG+=Paymetheus.Tests

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
	touch staging/common/deleteme
	$(foreach PMDIR,$(NUGETPKG),$(NUGET) install $(PMDIR)/packages.config;)
	cd $(PAYMETHEUS) && $(MSBUILD) -p:Configuration=Release -p:Platform=x86
	cd $(PAYMETHEUS) && $(MSBUILD) -p:Configuration=Release -p:Platform=x64
	(cd staging/x86 && GOARCH=386 go build $(PROJWALLET))
	(cd staging/x64 && GOARCH=amd64 go build $(PROJWALLET))
	$(foreach PROG,$(PROJFILES),cp -r $(PAYMETHEUS)/$(PROG) staging/x86/;)
	$(foreach PROG,$(PROJFILES64),cp -r $(PAYMETHEUS)/$(PROG) staging/x64/;)
	sh "$(TOOLSDIR)/sign-staging.sh" "$(CURDIR)/staging/x86/*.exe" "$(CURDIR)/staging/x64/*.exe"

harvest: stage
	$(HEAT) dir staging/common -nologo -gg -cg PaymetheusCommonFiles -sreg -sfrag \
	    -srd -suid -dr PAYMETHEUSROOT -var var.SourceDir -out staging/paymetheusproj.wxs
	$(HEAT) dir staging/x86 -nologo -gg -cg PAYMETHEUSArchFiles -sreg -sfrag -srd -suid \
	    -dr PAYMETHEUSROOT -var var.SourceDir -out staging/paymetheusarch_x86.wxs
	$(HEAT) dir staging/x64 -nologo -gg -cg PAYMETHEUSArchFiles -sreg -sfrag -srd -suid \
	    -dr PAYMETHEUSROOT -var var.SourceDir -out staging/paymetheusarch_x64.wxs

wixlib: paymetheus_common.wxs harvest
#	x86
	$(CANDLE) -nologo -arch x86 -dSourceDir=staging/common -o paymetheusproj_x86.wixobj \
	    staging/paymetheusproj.wxs
	$(CANDLE) -nologo -arch x86 -dSourceDir=staging/x86 -o paymetheusarch_x86.wixobj \
	    staging/paymetheusarch_x86.wxs
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" -o paymetheuscommon_x86.wixobj \
	    paymetheus_common.wxs
	$(LIT) -nologo -o paymetheus_$(VERNAME)_x86.wixlib paymetheuscommon_x86.wixobj \
	    paymetheusproj_x86.wixobj paymetheusarch_x86.wixobj -b .
#	x64
	$(CANDLE) -nologo -arch x64 -dSourceDir=staging/common -o paymetheusproj_x64.wixobj \
	    staging/paymetheusproj.wxs
	$(CANDLE) -nologo -arch x64 -dSourceDir=staging/x64 -o paymetheusarch_x64.wixobj \
	    staging/paymetheusarch_x64.wxs
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" -o paymetheuscommon_x64.wixobj \
	    paymetheus_common.wxs
	$(LIT) -nologo -o paymetheus_$(VERNAME)_x64.wixlib paymetheuscommon_x64.wixobj \
	    paymetheusproj_x64.wixobj paymetheusarch_x64.wixobj -b .


# Intentionally not built as we use wixlibs instead currently, but the logic
# is all there for proper build of merge modules should we ever need to
# provide to a third party.
msm: paymetheus_msm.wxs wixlib
#	x86
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" -o paymetheusmsm_x86.wixobj \
	    paymetheus_msm.wxs
	$(LIGHT) -nologo -o paymetheus_$(VERNAME)_x86.msm paymetheusmsm_x86.wixobj \
	    paymetheus_$(VERNAME)_x86.wixlib -b . -b ../resource
#	x64
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" -o paymetheusmsm_x64.wixobj \
	    paymetheus_msm.wxs
	$(LIGHT) -nologo -o paymetheus_$(VERNAME)_x64.msm paymetheusmsm_x64.wixobj \
	    paymetheus_$(VERNAME)_x64.wixlib -b . -b ../resource

msi: paymetheus_common.wxs wixlib
#	x86
	$(CANDLE) -nologo -arch x86 -dVersion="$(VERS)" \
	    -o paymetheus_standalone_x86.wixobj paymetheus_standalone.wxs
	$(LIGHT) -nologo -o paymetheus_standalone_$(VERNAME)_x86.msi \
	    paymetheus_standalone_x86.wixobj paymetheus_$(VERNAME)_x86.wixlib \
	    -ext WixUIExtension -b . -b ../resource
#	x64
	$(CANDLE) -nologo -arch x64 -dVersion="$(VERS)" \
	    -o paymetheus_standalone_x64.wixobj paymetheus_standalone.wxs
	$(LIGHT) -nologo -o paymetheus_standalone_$(VERNAME)_x64.msi \
	    paymetheus_standalone_x64.wixobj paymetheus_$(VERNAME)_x64.wixlib \
	    -ext WixUIExtension -b . -b ../resource
	sh "$(TOOLSDIR)/sign.sh" $(INSTALLERNAME) "$(CURDIR)/*.msi"


cleanobjs:
	rm -f *.wixobj *.wixpdb staging/*.wxs

clean: cleanobjs
	rm -rf staging
	rm -f paymetheus*.wixlib paymetheus*.msm paymetheus*.msi

.PHONY: all current stage harvest wixlib msm msi wixlib64 msm64 msi64 cleanobjs clean
