// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred.Wallet;
using System;
using System.Windows.Input;

namespace Paymetheus
{
    class ImportDialogViewModel : DialogViewModelBase
    {
        public ImportDialogViewModel(ShellViewModel shell, Account account) : base(shell)
        {
            _account = account;
            _import = new DelegateCommand(ImportAction);
        }

        private readonly Account _account;

        private DelegateCommand _import;
        public ICommand Import => _import;
        public string PrivateKeyWif { private get; set; } = "";
        public string Passphrase { private get; set; } = "";
        public bool Rescan { private get; set; } = true;

        private async void ImportAction()
        {
            // TODO: Consider returning the Task and passing to the shell, to consolidate
            // the display of all async error messages.
            try
            {
                _import.Executable = false;
                await App.Current.WalletRpcClient.ImportPrivateKeyAsync(_account, PrivateKeyWif, Rescan, Passphrase);
                HideDialog();
            }
            catch (Exception ex)
            {
                Console.WriteLine($"Failed to import key:\n{ex}");

                _import.Executable = true;
                CommandManager.InvalidateRequerySuggested();
            }
        }
    }
}
