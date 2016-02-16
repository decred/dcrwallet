// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

namespace Paymetheus.Decred.Wallet
{
    public sealed class AccountProperties
    {
        public string AccountName { get; set; }
        public Amount TotalBalance { get; set; }
        public Amount ImmatureCoinbaseReward { get; set; }
        public Amount ZeroConfSpendableBalance => TotalBalance - ImmatureCoinbaseReward;

        public uint ExternalKeyCount { get; set; }
        public uint InternalKeyCount { get; set; }
        public uint ImportedKeyCount { get; set; }
    }
}
