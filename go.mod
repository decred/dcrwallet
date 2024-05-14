module decred.org/dcrwallet/v4

go 1.20

require (
	decred.org/cspp/v2 v2.1.0
	github.com/decred/dcrd/addrmgr/v2 v2.0.2
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.0
	github.com/decred/dcrd/blockchain/standalone/v2 v2.2.0
	github.com/decred/dcrd/blockchain/v5 v5.0.0
	github.com/decred/dcrd/certgen v1.1.2
	github.com/decred/dcrd/chaincfg/chainhash v1.0.4
	github.com/decred/dcrd/chaincfg/v3 v3.2.0
	github.com/decred/dcrd/connmgr/v3 v3.1.1
	github.com/decred/dcrd/crypto/blake256 v1.0.1
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2
	github.com/decred/dcrd/dcrec v1.0.1
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.2.0
	github.com/decred/dcrd/dcrjson/v4 v4.0.1
	github.com/decred/dcrd/dcrutil/v4 v4.0.1
	github.com/decred/dcrd/gcs/v4 v4.0.0
	github.com/decred/dcrd/hdkeychain/v3 v3.1.1
	github.com/decred/dcrd/peer/v3 v3.0.2
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.1.0
	github.com/decred/dcrd/rpcclient/v8 v8.0.0
	github.com/decred/dcrd/txscript/v4 v4.1.0
	github.com/decred/dcrd/wire v1.6.0
	github.com/decred/dcrtest/dcrdtest v1.0.0
	github.com/decred/dcrtest/dcrwtest v1.0.0
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.2.0
	github.com/decred/vspd/client/v3 v3.0.0
	github.com/decred/vspd/types/v2 v2.1.0
	github.com/gorilla/websocket v1.5.0
	github.com/jessevdk/go-flags v1.5.0
	github.com/jrick/bitset v1.0.0
	github.com/jrick/logrotate v1.0.0
	github.com/jrick/wsrpc/v2 v2.3.5
	go.etcd.io/bbolt v1.3.8
	golang.org/x/crypto v0.7.0
	golang.org/x/sync v0.5.0
	golang.org/x/term v0.7.0
	google.golang.org/grpc v1.54.0
	google.golang.org/grpc/cmd/protoc-gen-go-grpc v1.2.0
	google.golang.org/protobuf v1.30.0
	matheusd.com/testctx v0.2.0
)

replace github.com/decred/dcrtest/dcrwtest => github.com/matheusd/dcrtest/dcrwtest v0.0.0-20231221151844-30516a66b790

require (
	decred.org/dcrwallet/v3 v3.0.0 // indirect
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/companyzero/sntrup4591761 v0.0.0-20220309191932-9e0f3af2f07a // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/decred/base58 v1.0.5 // indirect
	github.com/decred/dcrd v1.8.0 // indirect
	github.com/decred/dcrd/bech32 v1.1.3 // indirect
	github.com/decred/dcrd/container/apbf v1.0.1 // indirect
	github.com/decred/dcrd/database/v3 v3.0.1 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.3 // indirect
	github.com/decred/dcrd/lru v1.1.2 // indirect
	github.com/decred/dcrd/math/uint256 v1.0.1 // indirect
	github.com/decred/vspd/client/v2 v2.0.0 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/klauspost/cpuid/v2 v2.2.5 // indirect
	github.com/syndtr/goleveldb v1.0.1-0.20210819022825-2ae1ddf74ef7 // indirect
	golang.org/x/net v0.9.0 // indirect
	golang.org/x/sys v0.12.0 // indirect
	golang.org/x/text v0.9.0 // indirect
	google.golang.org/genproto v0.0.0-20230410155749-daa745c078e1 // indirect
	lukechampine.com/blake3 v1.2.1 // indirect
)
