DCRD=github.com/decred/dcrd
DCRWALLET=github.com/decred/dcrwallet
DCRTICKETBUYER=github.com/decred/dcrticketbuyer

CURRENTPAYMETHEUS = $(shell git rev-parse HEAD)

ifndef PAYMETHEUSDIGEST
$(error PAYMETHEUSDIGEST not set)
endif

ifndef DCRDDIGEST
$(error DCRDDIGEST not set)
endif

ifndef DCRWALLETDIGEST
$(error DCRWALLETDIGEST not set)
endif

ifndef DCRTICKETBUYERDIGEST
$(error DCRTICKETBUYERDIGEST not set)
endif

all: paymetheus dcrd dcrwallet dcrticketbuyer

clean:

current: $(GOPATH)/src/$(DCRD) $(GOPATH)/src/$(DCRWALLET) $(GOPATH)/src/$(DCRTICKETBUYER)
	cd $(GOPATH)/src/$(DCRD) && git checkout master
	cd $(GOPATH)/src/$(DCRD) && git pull --ff
	# wallet
	cd $(GOPATH)/src/$(DCRWALLET) && git checkout master
	cd $(GOPATH)/src/$(DCRWALLET) && git pull --ff
	cd $(GOPATH)/src/$(DCRWALLET) && glide install
	# ticketbuyer
	cd $(GOPATH)/src/$(DCRTICKETBUYER) && git checkout master
	cd $(GOPATH)/src/$(DCRTICKETBUYER) && git pull --ff
	cd $(GOPATH)/src/$(DCRTICKETBUYER) && glide install

paymetheus:
ifneq ($(CURRENTPAYMETHEUS),$(PAYMETHEUSDIGEST))
	@echo unexpected Paymetheus version $(PAYMETHEUSDIGEST)
	exit 1
endif

dcrd: $(GOPATH)/src/$(DCRD)
	cd $(GOPATH)/src/$(DCRD) && git checkout master
	cd $(GOPATH)/src/$(DCRD) && git pull --ff
	cd $(GOPATH)/src/$(DCRD) && git checkout $(DCRDDIGEST)
#	cd $(GOPATH)/src/$(DCRD) && glide install

$(GOPATH)/src/$(DCRD):
	go get -d -u -v $(DCRD)

dcrwallet: $(GOPATH)/src/$(DCRWALLET)
	cd $(GOPATH)/src/$(DCRWALLET) && git checkout master
	cd $(GOPATH)/src/$(DCRWALLET) && git pull --ff
	cd $(GOPATH)/src/$(DCRWALLET) && git checkout $(DCRWALLETDIGEST)
	cd $(GOPATH)/src/$(DCRWALLET) && glide install

$(GOPATH)/src/$(DCRWALLET):
	cd `dirname $(GOPATH)/src/$(DCRWALLET)` && git clone git@github.com:decred/`basename $(DCRWALLET)`.git

dcrticketbuyer: $(GOPATH)/src/$(DCRTICKETBUYER)
	cd $(GOPATH)/src/$(DCRTICKETBUYER) && git checkout master
	cd $(GOPATH)/src/$(DCRTICKETBUYER) && git pull --ff
	cd $(GOPATH)/src/$(DCRTICKETBUYER) && git checkout $(DCRTICKETBUYERDIGEST)
	cd $(GOPATH)/src/$(DCRTICKETBUYER) && glide install

$(GOPATH)/src/$(DCRTICKETBUYER):
	cd `dirname $(GOPATH)/src/$(DCRTICKETBUYER)` && git clone git@github.com:decred/`basename $(DCRTICKETBUYER)`.git

.PHONY: all clean dcrd dcrwallet dcrticketbuyer paymetheus
