// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;

namespace Paymetheus.Bitcoin.Wallet
{
    public struct TransactionSet
    {
        public TransactionSet(List<Block> minedTransactions, Dictionary<Sha256Hash, WalletTransaction> unminedTransactions)
        {
            if (minedTransactions == null)
                throw new ArgumentNullException(nameof(minedTransactions));
            if (unminedTransactions == null)
                throw new ArgumentNullException(nameof(unminedTransactions));

            MinedTransactions = minedTransactions;
            UnminedTransactions = unminedTransactions;
        }

        public List<Block> MinedTransactions { get; }
        public Dictionary<Sha256Hash, WalletTransaction> UnminedTransactions { get; }
    }
}