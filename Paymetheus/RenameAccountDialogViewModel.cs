// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Wallet;
using System;
using System.Windows.Input;

namespace Paymetheus
{
    sealed class RenameAccountDialogViewModel : DialogViewModelBase
    {
        public RenameAccountDialogViewModel(ShellViewModel shell, Account account, string currentName)
            : base(shell)
        {
            _account = account;
            _rename = new DelegateCommand(RenameAction);
            CurrentAccountName = currentName;
        }

        private readonly Account _account;

        private DelegateCommand _rename;
        public ICommand Rename => _rename;
        public string CurrentAccountName { get; }
        public string NewAccountName { private get; set; } = "";


        private async void RenameAction()
        {
            // TODO: Consider returning the Task and passing to the shell, to consolidate
            // the display of all async error messages.
            try
            {
                _rename.Executable = false;
                await App.Current.WalletRpcClient.RenameAccountAsync(_account, NewAccountName);
                HideDialog();
            }
            catch (Exception ex)
            {
                Console.WriteLine($"Failed to rename account:\n{ex}");

                _rename.Executable = true;
                CommandManager.InvalidateRequerySuggested();
            }
        }
    }
}
