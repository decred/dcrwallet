// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using System;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Input;

namespace Paymetheus
{
    sealed class PassphraseDialogViewModel : DialogViewModelBase
    {
        public PassphraseDialogViewModel(ShellViewModel shell, string header, string buttonText, Func<string, Task> executeWithPassphrase)
            : base(shell)
        {
            Header = header;
            ExecuteText = buttonText;
            _execute = executeWithPassphrase;
            Execute = new DelegateCommand(ExecuteAction);
        }

        private Func<string, Task> _execute;

        public string Header { get; }
        public string ExecuteText { get; }
        public string Passphrase { private get; set; } = "";

        public ICommand Execute { get; }

        private async void ExecuteAction()
        {
            try
            {
                await _execute(Passphrase);
                HideDialog();
            }
            catch (Exception ex)
            {
                MessageBox.Show(ex.ToString());
            }
        }
    }
}
