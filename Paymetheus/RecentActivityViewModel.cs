// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System.Collections.ObjectModel;

namespace Paymetheus
{
    sealed class RecentActivityViewModel : ViewModelBase
    {
        public RecentActivityViewModel(ShellViewModel shell) : base(shell) { }

        public ObservableCollection<TransactionViewModel> RecentTransactions { get; } =
            new ObservableCollection<TransactionViewModel>();
    }
}
