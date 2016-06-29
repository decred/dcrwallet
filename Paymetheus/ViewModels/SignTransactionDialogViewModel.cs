using Paymetheus.Framework;
using System;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Input;

namespace Paymetheus.ViewModels
{
    class SignTransactionDialogViewModel : DialogViewModelBase
    {
        public SignTransactionDialogViewModel(ShellViewModelBase shell, Func<string, Task> executeWithPassphrase)
            : base(shell)
        {
            _execute = executeWithPassphrase;
        }

        Func<string, Task> _execute;

        public string Passphrase { private get; set; } = "";

        public ICommand SignTransactionCommand { get; }

        private async void SignTransaction()
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
