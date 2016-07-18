// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;
using System.Linq;

namespace Paymetheus.Decred.Wallet
{
    public struct TransactionSet
    {
        public TransactionSet(List<Block> minedTransactions, Dictionary<Blake256Hash, WalletTransaction> unminedTransactions)
        {
            if (minedTransactions == null)
                throw new ArgumentNullException(nameof(minedTransactions));
            if (unminedTransactions == null)
                throw new ArgumentNullException(nameof(unminedTransactions));

            MinedTransactions = minedTransactions;
            UnminedTransactions = unminedTransactions;
        }

        public List<Block> MinedTransactions { get; }
        public Dictionary<Blake256Hash, WalletTransaction> UnminedTransactions { get; }

        public int TransactionCount() => MinedTransactions.SelectMany(b => b.Transactions).Count() + UnminedTransactions.Count;
    }
}