// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin.Script;
using System;

namespace Paymetheus.Bitcoin.Wallet
{
    public sealed class UnspentOutput
    {
        public UnspentOutput(Sha256Hash txHash, uint outputIndex, Amount amount, OutputScript pkScript, DateTimeOffset seenTime, bool isFromCoinbase)
        {
            TransactionHash = txHash;
            OutputIndex = outputIndex;
            Amount = amount;
            PkScript = pkScript;
            SeenTime = seenTime;
            IsFromCoinbase = IsFromCoinbase;
        }

        public Sha256Hash TransactionHash { get; }
        public uint OutputIndex { get; }
        public Amount Amount { get; }
        public OutputScript PkScript { get; }
        public DateTimeOffset SeenTime { get; }
        public DateTime LocalSeenTime => SeenTime.LocalDateTime;
        public bool IsFromCoinbase { get; }
    }
}
