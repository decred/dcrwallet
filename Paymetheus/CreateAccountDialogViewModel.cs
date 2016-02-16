// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Windows;
using System.Windows.Input;

namespace Paymetheus
{
    sealed class CreateAccountDialogViewModel : DialogViewModelBase
    {
        public CreateAccountDialogViewModel(ShellViewModel shell) : base(shell)
        {
            Execute = new DelegateCommand(ExecuteAction);
        }

        public string AccountName { get; set; } = "";
        public string Passphrase { private get; set; } = "";

        public ICommand Execute { get; }

        private async void ExecuteAction()
        {
            try
            {
                await App.Current.WalletRpcClient.NextAccountAsync(Passphrase, AccountName);
                HideDialog();
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message);
            }
        }
    }
}
