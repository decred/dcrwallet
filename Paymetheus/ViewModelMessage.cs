// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Wallet;

namespace Paymetheus
{
    class ViewModelMessageBase { }

    sealed class ShowTransactionMessage : ViewModelMessageBase
    {
        public ShowTransactionMessage(TransactionViewModel tx)
        {
            Transaction = tx;
        }

        public TransactionViewModel Transaction { get; }
    }

    sealed class ShowAccountMessage : ViewModelMessageBase
    {
        public ShowAccountMessage(Account account)
        {
            Account = account;
        }

        public Account Account { get; }
    }

    sealed class OpenDialogMessage : ViewModelMessageBase
    {
        public OpenDialogMessage(DialogViewModelBase dialog)
        {
            Dialog = dialog;
        }

        public DialogViewModelBase Dialog { get; }
    }

    sealed class HideDialogMessage : ViewModelMessageBase { }
}
