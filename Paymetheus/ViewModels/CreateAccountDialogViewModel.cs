// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Grpc.Core;
using Paymetheus.Framework;
using System;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Input;

namespace Paymetheus.ViewModels
{
    public sealed class CreateAccountDialogViewModel : DialogViewModelBase
    {
        public CreateAccountDialogViewModel(ShellViewModel shell) : base(shell)
        {
            Execute = new DelegateCommandAsync(ExecuteAction);
        }

        public string AccountName { get; set; } = "";
        public string Passphrase { private get; set; } = "";

        public ICommand Execute { get; }

        private async Task ExecuteAction()
        {
            try
            {
                await App.Current.Synchronizer.WalletRpcClient.NextAccountAsync(Passphrase, AccountName);
                HideDialog();
            }
            catch (RpcException ex) when (ex.Status.StatusCode == StatusCode.AlreadyExists)
            {
                MessageBox.Show("Account name already exists");
            }
            catch (RpcException ex) when (ex.Status.StatusCode == StatusCode.InvalidArgument)
            {
                // Since there is no client-side validation of account name user input, this might be an
                // invalid account name or the wrong passphrase.  Just show the detail for now.
                MessageBox.Show(ex.Status.Detail);
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.Message, "Error");
            }
        }
    }
}
