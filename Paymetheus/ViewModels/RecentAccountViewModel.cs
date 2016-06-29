// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Decred.Wallet;
using Paymetheus.Framework;
using System;

namespace Paymetheus.ViewModels
{
    sealed class RecentAccountViewModel : ViewModelBase
    {
        public RecentAccountViewModel(Account account, AccountProperties properties) : base()
        {
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

        public string BalanceString => Denomination.Decred.FormatAmount(Balance);
    }
}
