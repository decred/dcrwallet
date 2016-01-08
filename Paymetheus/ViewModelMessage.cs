// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Bitcoin.Wallet;

namespace Paymetheus
{
    class ViewModelMessage { }

    class ShowTransactionMessage : ViewModelMessage
    {
        public ShowTransactionMessage(TransactionViewModel tx)
        {
            Transaction = tx;
        }

        public TransactionViewModel Transaction { get; }
    }

    class ShowAccountMessage : ViewModelMessage
    {
        public ShowAccountMessage(Account account)
        {
            Account = account;
        }

        public Account Account { get; }
    }

    class OpenDialogMessage : ViewModelMessage
    {
        public OpenDialogMessage(DialogViewModelBase dialog)
        {
            Dialog = dialog;
        }

        public DialogViewModelBase Dialog { get; }
    }

    class HideDialogMessage : ViewModelMessage { }
}
