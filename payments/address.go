package payments

// Address is a human-readable encoding of an output script.
//
// Address encodings may include a network identifier, to prevent misuse on an
// alternate Decred network.
type Address interface {
	String() string

	// PaymentScript returns the output script and script version to pay the
	// address.  The version is always returned with the script, as it is
	// not useful to use the script without the version.
	PaymentScript() (version uint16, script []byte)

	// ScriptLen returns the known length of the address output script.
	ScriptLen() int
}

// PubkeyHashAddress is an address which pays to the hash of a public key.
type PubkeyHashAddress interface {
	Address

	PubkeyHash() []byte
}

// PubkeyAddress is an address which pays to a public key.  These addresses are
// typically avoided in favor of pubkey hash addresses, unless an operation
// (e.g. generating a multisig script) requires knowing the unhashed public key.
type PubkeyAddress interface {
	Address

	Pubkey() []byte
}

// ScriptHashAddress is an address which pays to the hash of a redeem script
// (pay-to-script-hash, or P2SH).  The redeem script is provided by the
// redeeming input scripts as a final data push.
//
// An implementation of ScriptHashAddress does not necessarily know the unhashed
// redeem script, and may only know how to pay to the address.
type ScriptHashAddress interface {
	Address

	ScriptHash() []byte
}

// MultisigAddress is a P2SH address with a known multisig redeem script.  A
// multisig output requires M out of any N valid signatures from N different
// keypairs in order to redeem the output.
//
// Bare multisig output scripts are nonstandard, and so this is only able to
// implement Address as a P2SH address.  Any P2SH address with an unknown redeem
// script may be a multisig address, but it is impossible to know until the
// redeem script is revealed.
type MultisigAddress interface {
	ScriptHashAddress

	// RedeemScript returns the unhashed multisig script that redeemers must
	// provide as the final data push
	RedeemScript() []byte

	// Pubkeys returns all N public keys for each keypair.
	Pubkeys() [][]byte

	// M is the required number of unique signatures needed to redeem
	// outputs paying the address.
	M() int

	// N is the total number of public/private keypairs.  The return value
	// must always equal len(Pubkeys()).
	N() int
}
