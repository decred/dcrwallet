// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Collections.Generic;

namespace Paymetheus.Decred.Wallet
{
    public sealed class WalletChanges
    {
        public WalletChanges(HashSet<Blake256Hash> detachedBlocks,
            List<Block> attachedBlocks,
            List<WalletTransaction> newUnminedTransactions,
            HashSet<Blake256Hash> allUnminedHashes)
        {
            if (detachedBlocks == null)
                throw new ArgumentNullException(nameof(detachedBlocks));
            if (attachedBlocks == null)
                throw new ArgumentNullException(nameof(attachedBlocks));
            if (newUnminedTransactions == null)
                throw new ArgumentNullException(nameof(newUnminedTransactions));
            if (allUnminedHashes == null)
                throw new ArgumentNullException(nameof(allUnminedHashes));

            DetachedBlocks = detachedBlocks;
            AttachedBlocks = attachedBlocks;
            NewUnminedTransactions = newUnminedTransactions;
            AllUnminedHashes = allUnminedHashes;
        }

        public HashSet<Blake256Hash> DetachedBlocks { get; }
        public List<Block> AttachedBlocks { get; }
        public List<WalletTransaction> NewUnminedTransactions { get; }
        public HashSet<Blake256Hash> AllUnminedHashes { get; }
    }
}
