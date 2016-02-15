// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin;
using Paymetheus.Bitcoin.Wallet;
using System;

namespace Paymetheus
{
    sealed class RecentAccountViewModel : ViewModelBase
    {
        public RecentAccountViewModel(ViewModelBase parent, Account account, AccountProperties properties)
            : base(parent)
        {
            if (parent == null)
                throw new ArgumentNullException(nameof(parent));
            if (properties == null)
                throw new ArgumentNullException(nameof(properties));

            Account = account;
            AccountName = properties.AccountName;
            Balance = properties.TotalBalance;
        }

        public Account Account { get; }
        private string _accountName;
        public string AccountName
        {
            get { return _accountName; }
            set { if (_accountName != value) { _accountName = value; RaisePropertyChanged(); } }
        }

        private Amount _balance;
        public Amount Balance
        {
            get { return _balance; }
            set
            {
                _balance = value;
                RaisePropertyChanged();
                RaisePropertyChanged(nameof(BalanceString));
            }
        }

        public string BalanceString => Denomination.Bitcoin.FormatAmount(Balance);
    }
}
