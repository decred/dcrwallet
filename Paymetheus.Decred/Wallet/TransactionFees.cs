// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Script;
using System;
using System.Linq;

namespace Paymetheus.Decred.Wallet
{
    public static class TransactionFees
    {
        public static readonly Amount DefaultFeePerKb = 1000000;

        public static Amount FeeForSerializeSize(Amount feePerKb, int txSerializeSize)
        {
            if (feePerKb < 0)
                throw Errors.RequireNonNegative(nameof(feePerKb));
            if (txSerializeSize < 0)
                throw Errors.RequireNonNegative(nameof(txSerializeSize));

            var fee = feePerKb * txSerializeSize / 1000;

            if (fee == 0 && feePerKb > 0)
                fee = feePerKb;

            if (!TransactionRules.IsSaneOutputValue(fee))
                throw new TransactionRuleException($"Fee of {fee} is invalid");

            return fee;
        }

        public static Amount ActualFee(Transaction tx, Amount totalInput)
        {
            if (tx == null)
                throw new ArgumentNullException(nameof(tx));
            if (totalInput < 0)
                throw Errors.RequireNonNegative(nameof(totalInput));

            var totalOutput = tx.Outputs.Sum(o => o.Amount);
            return totalInput - totalOutput;
        }

        public static Amount EstimatedFeePerKb(Transaction tx, Amount totalInput)
        {
            if (tx == null)
                throw new ArgumentNullException(nameof(tx));
            if (totalInput < 0)
                throw Errors.RequireNonNegative(nameof(totalInput));

            var estimatedSize = Transaction.EstimateSerializeSize(tx.Inputs.Length, tx.Outputs, false);
            var actualFee = ActualFee(tx, totalInput);
            return actualFee * 1000 / estimatedSize;
        }
    }
}