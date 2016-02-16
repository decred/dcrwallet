// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Decred.Script;
using Paymetheus.Decred.Util;
using Paymetheus.Decred.Wallet;
using System.Linq;
using Walletrpc;

namespace Paymetheus.Rpc
{
    internal static class Marshalers
    {
        public static WalletTransaction MarshalWalletTransaction(TransactionDetails tx)
        {
            var transaction = Transaction.Deserialize(tx.Transaction.ToByteArray());
            var hash = new Blake256Hash(tx.Hash.ToByteArray());
            var inputs = tx.Debits
                .Select(i => new WalletTransaction.Input(i.PreviousAmount, new Account(i.PreviousAccount)))
                .ToArray();
            var outputs = tx.Outputs
                .Select((o, index) =>
                {
                    // There are two kinds of transactions to care about when choosing which outputs
                    // should be created: transactions created by other wallets (inputs.Length == 0)
                    // and those that spend controlled outputs from this wallet (inputs.Length != 0).
                    // If the transaction was created by this wallet, then all outputs (both controlled
                    // and uncontrolled) should be included.  Otherwise, uncontrolled outputs can be
                    // ignored since they are not relevant (they could be change outputs for the other
                    // wallet or outputs created for another unrelated wallet).
                    if (o.Mine || inputs.Length != 0)
                        return MarshalWalletTransactionOutput(o, transaction.Outputs[index]);
                    else
                        return null;
                })
                .Where(o => o != null)
                .ToArray();
            var fee = inputs.Length == transaction.Inputs.Length ? (Amount?)tx.Fee : null;
            var seenTime = DateTimeOffsetExtras.FromUnixTimeSeconds(tx.Timestamp);

            return new WalletTransaction(transaction, hash, inputs, outputs, fee, seenTime);
        }

        private static WalletTransaction.Output MarshalWalletTransactionOutput(TransactionDetails.Types.Output o, Transaction.Output txOutput)
        {
            if (o.Mine)
                return new WalletTransaction.Output.ControlledOutput(txOutput.Amount, new Account(o.Account), o.Internal);
            else
                return new WalletTransaction.Output.UncontrolledOutput(txOutput.Amount, txOutput.PkScript);
        }

        public static Block MarshalBlock(BlockDetails b)
        {
            var hash = new Blake256Hash(b.Hash.ToByteArray());
            var height = b.Height;
            var unixTime = b.Timestamp;
            var transactions = b.Transactions.Select(MarshalWalletTransaction).ToList();

            return new Block(hash, height, unixTime, transactions);
        }

        public static UnspentOutput MarshalUnspentOutput(FundTransactionResponse.Types.PreviousOutput o)
        {
            var txHash = new Blake256Hash(o.TransactionHash.ToByteArray());
            var outputIndex = o.OutputIndex;
            var amount = (Amount)o.Amount;
            var pkScript = OutputScript.ParseScript(o.PkScript.ToByteArray());
            var seenTime = DateTimeOffsetExtras.FromUnixTimeSeconds(o.ReceiveTime);
            var isFromCoinbase = o.FromCoinbase;

            return new UnspentOutput(txHash, outputIndex, amount, pkScript, seenTime, isFromCoinbase);
        }
    }
}
