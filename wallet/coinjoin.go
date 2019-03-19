package wallet

import (
	"bytes"
	"context"
	"crypto/subtle"

	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/txscript/v2"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/errors/v2"
	"github.com/decred/dcrwallet/wallet/v3/walletdb"
)

type missingGenError struct{}

var errMissingGen missingGenError

func (missingGenError) Error() string   { return "coinjoin is missing gen output" }
func (missingGenError) MissingMessage() {}

type csppJoin struct {
	tx            *wire.MsgTx
	txInputs      map[wire.OutPoint]int
	myPrevScripts [][]byte
	myIns         []*wire.TxIn
	change        *wire.TxOut
	mcount        int
	genScripts    [][]byte
	genIndex      []int
	amount        int64
	wallet        *Wallet
	mixAccount    uint32
	mixBranch     uint32

	ctx context.Context
}

func (w *Wallet) newCsppJoin(ctx context.Context, change *wire.TxOut, amount dcrutil.Amount, mixAccount, mixBranch uint32, mcount int) *csppJoin {
	cj := &csppJoin{
		tx:         &wire.MsgTx{Version: 1},
		change:     change,
		mcount:     mcount,
		amount:     int64(amount),
		wallet:     w,
		mixAccount: mixAccount,
		mixBranch:  mixBranch,
		ctx:        ctx,
	}
	if change != nil {
		cj.tx.TxOut = append(cj.tx.TxOut, change)
	}
	return cj
}

func (c *csppJoin) addTxIn(prevScript []byte, in *wire.TxIn) {
	c.tx.TxIn = append(c.tx.TxIn, in)
	c.myPrevScripts = append(c.myPrevScripts, prevScript)
	c.myIns = append(c.myIns, in)
}

func (c *csppJoin) Gen() ([][]byte, error) {
	const op errors.Op = "cspp.Gen"
	gen := make([][]byte, c.mcount)
	c.genScripts = make([][]byte, c.mcount)
	var updates []func(walletdb.ReadWriteTx) error
	for i := 0; i < c.mcount; i++ {
		persist := c.wallet.deferPersistReturnedChild(c.ctx, &updates)
		mixAddr, err := c.wallet.nextAddress(c.ctx, op, persist, c.mixAccount, c.mixBranch, WithGapPolicyIgnore())
		if err != nil {
			return nil, err
		}
		script, version, err := addressScript(mixAddr)
		if err != nil {
			return nil, err
		}
		if version != 0 {
			return nil, errors.E("expected script version 0")
		}
		c.genScripts[i] = script
		gen[i] = mixAddr.Hash160()[:]
	}
	err := walletdb.Update(c.ctx, c.wallet.db, func(dbtx walletdb.ReadWriteTx) error {
		for _, f := range updates {
			if err := f(dbtx); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, errors.E(op, err)
	}
	return gen, nil
}

func (c *csppJoin) Confirm() error {
	const op errors.Op = "cspp.Confirm"
	err := walletdb.View(c.ctx, c.wallet.db, func(dbtx walletdb.ReadTx) error {
		addrmgrNs := dbtx.ReadBucket(waddrmgrNamespaceKey)
		for outx, in := range c.myIns {
			outScript := c.myPrevScripts[outx]
			index, ok := c.txInputs[in.PreviousOutPoint]
			if !ok {
				return errors.E("coinjoin is missing inputs")
			}
			in = c.tx.TxIn[index]

			const scriptVersion = 0
			_, addrs, _, err := txscript.ExtractPkScriptAddrs(scriptVersion, outScript, c.wallet.chainParams)
			if err != nil {
				return err
			}
			if len(addrs) != 1 {
				continue
			}
			apkh, ok := addrs[0].(*dcrutil.AddressPubKeyHash)
			if !ok {
				return errors.E(errors.Bug, "previous output is not P2PKH")
			}
			privKey, done, err := c.wallet.Manager.PrivateKey(addrmgrNs, apkh)
			if err != nil {
				return err
			}
			defer done()
			sigscript, err := txscript.SignatureScript(c.tx, index, outScript,
				txscript.SigHashAll, privKey, true)
			if err != nil {
				return errors.E(errors.Op("txscript.SignatureScript"), err)
			}
			in.SignatureScript = sigscript
		}
		return nil
	})
	if err != nil {
		return errors.E(op, err)
	}
	return nil
}

func (c *csppJoin) mixOutputIndexes() []int {
	return c.genIndex
}

func (c *csppJoin) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)
	buf.Grow(c.tx.SerializeSize())
	err := c.tx.Serialize(buf)
	return buf.Bytes(), err
}

func (c *csppJoin) UnmarshalBinary(b []byte) error {
	tx := new(wire.MsgTx)
	err := tx.Deserialize(bytes.NewReader(b))
	if err != nil {
		return err
	}

	// Ensure all unmixed inputs, unmixed outputs, and mixed outputs exist.
	// Mixed outputs must be searched in constant time to avoid sidechannel leakage.
	txInputs := make(map[wire.OutPoint]int, len(tx.TxIn))
	for i, in := range tx.TxIn {
		txInputs[in.PreviousOutPoint] = i
	}
	var n int
	for _, in := range c.myIns {
		if index, ok := txInputs[in.PreviousOutPoint]; ok {
			other := tx.TxIn[index]
			if in.Sequence != other.Sequence || in.ValueIn != other.ValueIn {
				break
			}
			n++
		}
	}
	if n != len(c.myIns) {
		return errors.E("coinjoin is missing inputs")
	}
	if c.change != nil {
		var hasChange bool
		for _, out := range tx.TxOut {
			if out.Value != c.change.Value {
				continue
			}
			if out.Version != c.change.Version {
				continue
			}
			if !bytes.Equal(out.PkScript, c.change.PkScript) {
				continue
			}
			hasChange = true
			break
		}
		if !hasChange {
			return errors.E("coinjoin is missing change")
		}
	}
	indexes, err := constantTimeOutputSearch(tx, c.amount, 0, c.genScripts)
	if err != nil {
		return err
	}

	c.tx = tx
	c.txInputs = txInputs
	c.genIndex = indexes
	return nil
}

// constantTimeOutputSearch searches for the output indexes of mixed outputs to
// verify inclusion in a coinjoin.  It is constant time such that, for each
// searched script, all outputs with equal value, script versions, and script
// lengths matching the searched output are checked in constant time.
func constantTimeOutputSearch(tx *wire.MsgTx, value int64, scriptVer uint16, scripts [][]byte) ([]int, error) {
	var scan []int
	for i, out := range tx.TxOut {
		if out.Value != value {
			continue
		}
		if out.Version != scriptVer {
			continue
		}
		if len(out.PkScript) != len(scripts[0]) {
			continue
		}
		scan = append(scan, i)
	}
	indexes := make([]int, 0, len(scan))
	var missing int
	for _, s := range scripts {
		idx := -1
		for _, i := range scan {
			eq := subtle.ConstantTimeCompare(tx.TxOut[i].PkScript, s)
			idx = subtle.ConstantTimeSelect(eq, i, idx)
		}
		indexes = append(indexes, idx)
		eq := subtle.ConstantTimeEq(int32(idx), -1)
		missing = subtle.ConstantTimeSelect(eq, 1, missing)
	}
	if missing == 1 {
		return nil, errMissingGen
	}
	return indexes, nil
}
