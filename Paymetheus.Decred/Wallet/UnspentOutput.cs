// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Script;
using System;

namespace Paymetheus.Decred.Wallet
{
    public sealed class UnspentOutput
    {
        public UnspentOutput(Blake256Hash txHash, uint outputIndex, byte tree, Amount amount, OutputScript pkScript, DateTimeOffset seenTime, bool isFromCoinbase)
        {
            if (txHash == null)
                throw new ArgumentNullException(nameof(txHash));
            if (pkScript == null)
                throw new ArgumentNullException(nameof(pkScript));

            TransactionHash = txHash;
            OutputIndex = outputIndex;
            Tree = tree;
            Amount = amount;
            PkScript = pkScript;
            SeenTime = seenTime;
            IsFromCoinbase = IsFromCoinbase;
        }

        public Blake256Hash TransactionHash { get; }
        public uint OutputIndex { get; }
        public byte Tree { get; }
        public Amount Amount { get; }
        public OutputScript PkScript { get; }
        public DateTimeOffset SeenTime { get; }
        public DateTime LocalSeenTime => SeenTime.LocalDateTime;
        public bool IsFromCoinbase { get; }
    }
}
