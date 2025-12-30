module decred.org/dcrwallet/v5

go 1.24.0

require (
	decred.org/cspp/v2 v2.4.0
	github.com/decred/dcrd/addrmgr/v3 v3.0.0
	github.com/decred/dcrd/blockchain/stake/v5 v5.0.2
	github.com/decred/dcrd/blockchain/standalone/v2 v2.2.2
	github.com/decred/dcrd/blockchain/v5 v5.1.0
	github.com/decred/dcrd/certgen v1.2.0
	github.com/decred/dcrd/chaincfg/chainhash v1.0.5
	github.com/decred/dcrd/chaincfg/v3 v3.3.0
	github.com/decred/dcrd/connmgr/v3 v3.1.3
	github.com/decred/dcrd/crypto/blake256 v1.1.0
	github.com/decred/dcrd/crypto/rand v1.0.1
	github.com/decred/dcrd/crypto/ripemd160 v1.0.2
	github.com/decred/dcrd/dcrec v1.0.1
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.0
	github.com/decred/dcrd/dcrjson/v4 v4.2.0
	github.com/decred/dcrd/dcrutil/v4 v4.0.3
	github.com/decred/dcrd/gcs/v4 v4.1.1
	github.com/decred/dcrd/hdkeychain/v3 v3.1.3
	github.com/decred/dcrd/mixing v0.6.1
	github.com/decred/dcrd/rpc/jsonrpc/types/v4 v4.4.0
	github.com/decred/dcrd/rpcclient/v8 v8.1.0
	github.com/decred/dcrd/txscript/v4 v4.1.2
	github.com/decred/dcrd/wire v1.7.2
	github.com/decred/go-socks v1.1.0
	github.com/decred/slog v1.2.0
	github.com/decred/vspd/client/v4 v4.0.2
	github.com/decred/vspd/types/v3 v3.0.0
	github.com/gorilla/websocket v1.5.1
	github.com/jessevdk/go-flags v1.5.0
	github.com/jrick/bitset v1.0.0
	github.com/jrick/logrotate v1.0.0
	github.com/jrick/wsrpc/v2 v2.3.8
	go.etcd.io/bbolt v1.3.12
	golang.org/x/crypto v0.45.0
	golang.org/x/sync v0.18.0
	golang.org/x/term v0.37.0
	google.golang.org/grpc v1.76.0
	google.golang.org/protobuf v1.36.10
)

require (
	github.com/agl/ed25519 v0.0.0-20170116200512-5312a6153412 // indirect
	github.com/companyzero/sntrup4591761 v0.0.0-20220309191932-9e0f3af2f07a // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/decred/base58 v1.0.6 // indirect
	github.com/decred/dcrd/container/lru v1.0.0 // indirect
	github.com/decred/dcrd/database/v3 v3.0.3 // indirect
	github.com/decred/dcrd/dcrec/edwards/v2 v2.0.4 // indirect
	github.com/klauspost/cpuid/v2 v2.2.8 // indirect
	golang.org/x/mod v0.29.0 // indirect
	golang.org/x/net v0.47.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/tools v0.38.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250804133106-a7a43d27e69b // indirect
	google.golang.org/grpc/cmd/protoc-gen-go-grpc v1.5.1 // indirect
	lukechampine.com/blake3 v1.3.0 // indirect
)

tool (
	golang.org/x/tools/cmd/stringer
	google.golang.org/grpc/cmd/protoc-gen-go-grpc
	google.golang.org/protobuf/cmd/protoc-gen-go
)
