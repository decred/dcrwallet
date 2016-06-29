// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Framework;
using System.Collections.ObjectModel;

namespace Paymetheus.ViewModels
{
    public class OverviewViewModel : ViewModelBase
    {
        private int _accountsCount;
        public int AccountsCount
        {
            get { return _accountsCount; }
            set { _accountsCount = value; RaisePropertyChanged(); }
        }

        // TODO: Wallet does not expose any information such as the total amounts "sent" or "received".
        // Calculating that on the fly is infeasible with the wallet's current design, as it requires
        // iterating over every transaction and looking up whether every controlled output script is
        // controlled by the account's internal or external branch.  Should these be removed from the
        // view?

        // TODO: this isn't set or updated by anything.
        private int _transactionCount;
        public int TransactionCount
        {
            get { return _transactionCount; }
            set { _transactionCount = value; RaisePropertyChanged(); }
        }

        // TODO: the recent transactions list contains the following fields which are not associated
        // with a transaction, but the transaction's inputs and outputs:
        //
        //   - category icon (send/received/etc.)
        //   - account
        //   - address
        //   - debit/credit
        //
        // As an example of the problem this can cause by trying to display this information, consider
        // a transaction that spends from one account and moves value to another wallet's account.
        // Is the transaction a send or receive?  Which account is the transaction associated with?
        // Or, consider a transaction that contains two controlled outputs with different scripts.
        // Which address should be displayed?
        //
        // Displaying the address also goes against our rule of hiding addresses except for the use
        // of requesting a payment.
        //
        // Additionally, it would make a lot of sense to put the transaction hash here.
        //
        // This probably needs a redesigned view, where the transaction hash replaces the address,
        // and instead of a single amount, several amounts are listed with the account's name (or
        // "outgoing" or a shortened address, if sent to a non-wallet address) next to each amount.
        // The TransactionViewModel class already contains grouped outputs containing this information.
        // As for the icon, we can probably get away with considering any regular transaction spending
        // wallet outputs as a "send", and all other regular transaction "receives".  This would mean
        // that a transaction that only involves wallet outputs and addresses is considered a send, not
        // a receive.
        public ObservableCollection<TransactionViewModel> RecentTransactions { get; } =
            new ObservableCollection<TransactionViewModel>();
    }
}
