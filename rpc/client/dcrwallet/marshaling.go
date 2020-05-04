package dcrwallet

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	"github.com/decred/dcrd/hdkeychain/v3"
	dcrdtypes "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/wire"
)

type marshalJSONFunc func() ([]byte, error)
type unmarshalJSONFunc func([]byte) error

func (f marshalJSONFunc) MarshalJSON() ([]byte, error)    { return f() }
func (f *unmarshalJSONFunc) UnmarshalJSON(j []byte) error { return (*f)(j) }

func addressArrayMarshaler(n int, s func(i int) string) json.Marshaler {
	return marshalJSONFunc(func() ([]byte, error) {
		// Make buffer of estimated needed size.  Base58 Hash160
		// addresses are typically 35 characters long, plus 3 additional
		// characters per item for string quotes and comma.  Minimum two
		// characters are needed for the outer [].
		buf := new(bytes.Buffer)
		buf.Grow(2 + n*(3+35))

		buf.WriteByte('[')
		for i := 0; i < n; i++ {
			if i != 0 {
				buf.WriteByte(',')
			}
			buf.WriteByte('"')
			buf.WriteString(s(i))
			buf.WriteByte('"')
		}
		buf.WriteByte(']')

		return buf.Bytes(), nil
	})
}

func marshalAddresses(addrs []dcrutil.Address) json.Marshaler {
	return addressArrayMarshaler(len(addrs), func(i int) string {
		return addrs[i].String()
	})
}

func marshalTx(tx *wire.MsgTx) json.Marshaler {
	return marshalJSONFunc(func() ([]byte, error) {
		s := new(strings.Builder)
		err := tx.Serialize(s)
		if err != nil {
			return nil, err
		}
		return json.Marshal(s.String())
	})
}

func unmarshalOutpoints(ops *[]*wire.OutPoint) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		var array []dcrdtypes.TransactionInput
		err := json.Unmarshal(j, &array)
		if err != nil {
			return err
		}
		*ops = make([]*wire.OutPoint, 0, len(array))
		for i := range array {
			hash, err := chainhash.NewHashFromStr(array[i].Txid)
			if err != nil {
				return err
			}
			*ops = append(*ops, wire.NewOutPoint(hash, array[i].Vout, array[i].Tree))
		}
		return nil
	})
	return &f
}

func unmarshalHash(hash **chainhash.Hash) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		var s string
		err := json.Unmarshal(j, &s)
		if err != nil {
			return err
		}
		*hash, err = chainhash.NewHashFromStr(s)
		return err
	})
	return &f
}

func unmarshalHashes(hashes *[]*chainhash.Hash) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		var array []string
		err := json.Unmarshal(j, &array)
		if err != nil {
			return err
		}
		*hashes = make([]*chainhash.Hash, 0, len(array))
		for i := range array {
			hash, err := chainhash.NewHashFromStr(array[i])
			if err != nil {
				return err
			}
			*hashes = append(*hashes, hash)
		}
		return nil
	})
	return &f
}

func unmarshalAddress(addr *dcrutil.Address, net *chaincfg.Params) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		var s string
		err := json.Unmarshal(j, &s)
		if err != nil {
			return err
		}
		a, err := dcrutil.DecodeAddress(s, net)
		if err != nil {
			return err
		}
		*addr = a
		return nil
	})
	return &f
}

func unmarshalAddresses(addrs *[]dcrutil.Address, net *chaincfg.Params) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		var array []string
		err := json.Unmarshal(j, &array)
		if err != nil {
			return err
		}
		*addrs = make([]dcrutil.Address, 0, len(array))
		for i := range array {
			a, err := dcrutil.DecodeAddress(array[i], net)
			if err != nil {
				return err
			}
			*addrs = append(*addrs, a)
		}
		return nil
	})
	return &f
}

func unmarshalListAccounts(accounts map[string]dcrutil.Amount) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		object := make(map[string]float64)
		err := json.Unmarshal(j, &object)
		if err != nil {
			return err
		}
		for account, amount := range object {
			atoms, err := dcrutil.NewAmount(amount)
			if err != nil {
				return err
			}
			accounts[account] = atoms
		}
		return nil
	})
	return &f
}

func unmarshalHDKey(key **hdkeychain.ExtendedKey, net *chaincfg.Params) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		var s string
		err := json.Unmarshal(j, &s)
		if err != nil {
			return err
		}
		*key, err = hdkeychain.NewKeyFromString(s, net)
		return err
	})
	return &f
}

func unmarshalAmount(amount *dcrutil.Amount) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		var number float64
		err := json.Unmarshal(j, &number)
		if err != nil {
			return err
		}
		*amount, err = dcrutil.NewAmount(number)
		return err
	})
	return &f
}

func unmarshalWIF(wif **dcrutil.WIF, net *chaincfg.Params) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		var s string
		err := json.Unmarshal(j, &s)
		if err != nil {
			return err
		}
		*wif, err = dcrutil.DecodeWIF(s, net.PrivateKeyID)
		return nil
	})
	return &f
}

func unmarshalTx(tx **wire.MsgTx) json.Unmarshaler {
	f := unmarshalJSONFunc(func(j []byte) error {
		var s string
		err := json.Unmarshal(j, &s)
		if err != nil {
			return err
		}
		*tx = new(wire.MsgTx)
		return (*tx).Deserialize(hex.NewDecoder(strings.NewReader(s)))
	})
	return &f
}
