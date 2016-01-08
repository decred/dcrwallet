// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin;
using Paymetheus.Bitcoin.Script;
using Paymetheus.Bitcoin.Util;
using Paymetheus.Bitcoin.Wallet;
using System.Linq;
using Walletrpc;

namespace Paymetheus.Rpc
{
    internal static class Marshalers
    {
        public static WalletTransaction MarshalWalletTransaction(TransactionDetails tx)
        {
            var transaction = Transaction.Deserialize(tx.Transaction.ToByteArray());
            var hash = new Sha256Hash(tx.Hash.ToByteArray());
            var inputs = tx.Debits
                .Select(i => new WalletTransaction.Input(i.PreviousAmount, new Account(i.PreviousAccount)))
                .ToArray();
            var outputs = tx.Outputs
                .Select((o, index) => MarshalWalletTransactionOutput(o, transaction.Outputs[index]))
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
            var hash = new Sha256Hash(b.Hash.ToByteArray());
            var height = b.Height;
            var unixTime = b.Timestamp;
            var transactions = b.Transactions.Select(MarshalWalletTransaction).ToList();

            return new Block(hash, height, unixTime, transactions);
        }

        public static UnspentOutput MarshalUnspentOutput(FundTransactionResponse.Types.PreviousOutput o)
        {
            var txHash = new Sha256Hash(o.TransactionHash.ToByteArray());
            var outputIndex = o.OutputIndex;
            var amount = (Amount)o.Amount;
            var pkScript = OutputScript.ParseScript(o.PkScript.ToByteArray());
            var seenTime = DateTimeOffsetExtras.FromUnixTimeSeconds(o.ReceiveTime);
            var isFromCoinbase = o.FromCoinbase;

            return new UnspentOutput(txHash, outputIndex, amount, pkScript, seenTime, isFromCoinbase);
        }
    }
}
