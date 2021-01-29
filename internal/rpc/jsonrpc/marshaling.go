package jsonrpc

import (
	"bytes"
	"encoding/json"

	"decred.org/dcrwallet/v2/wallet"
)

type marshalJSONFunc func() ([]byte, error)

func (f marshalJSONFunc) MarshalJSON() ([]byte, error) { return f() }

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

func knownAddressMarshaler(addrs []wallet.KnownAddress) json.Marshaler {
	return addressArrayMarshaler(len(addrs), func(i int) string {
		return addrs[i].String()
	})
}

func addressStringsMarshaler(addrs []string) json.Marshaler {
	return addressArrayMarshaler(len(addrs), func(i int) string {
		return addrs[i]
	})
}
