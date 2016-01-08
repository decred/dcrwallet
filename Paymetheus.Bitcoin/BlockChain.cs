// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;

namespace Paymetheus.Bitcoin
{
    public static class BlockChain
    {
        public const int CoinbaseMaturity = 100;
        public const int MinCoinbaseScriptLength = 2;
        public const int MaxCoinbaseScriptLength = 100;

        public static bool IsCoinbase(Transaction tx)
        {
            if (tx == null)
                throw new ArgumentNullException(nameof(tx));

            if (tx.Inputs.Length != 1)
                return false;

            var previousOutput = tx.Inputs[0].PreviousOutpoint;
            return previousOutput.Index == uint.MaxValue && previousOutput.Hash.Equals(Sha256Hash.Zero);
        }

        public static int Confirmations(int blockChainHeight, BlockIdentity location)
        {
            if (location == null)
                throw new ArgumentNullException(nameof(location));

            if (location.IsUnmined())
                return 0;
            else
                return blockChainHeight - location.Height + 1;
        }

        public static int Confirmations(int blockChainHeight, int txHeight)
        {
            if (txHeight == -1 || txHeight > blockChainHeight)
                return 0;
            else
                return blockChainHeight - txHeight + 1;
        }

        public static int ConfirmationHeight(int blockChainHeight, int minConf)
        {
            return blockChainHeight - minConf + 1;
        }

        public static bool IsConfirmed(int blockchainHeight, int txHeight, int minConfirmations)
        {
            return Confirmations(blockchainHeight, txHeight) >= minConfirmations;
        }

        public static bool IsMatured(int blockchainHeight, int txHeight)
        {
            return IsConfirmed(blockchainHeight, txHeight, CoinbaseMaturity);
        }
    }
}
