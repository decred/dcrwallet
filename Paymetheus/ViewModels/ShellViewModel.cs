// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

using Paymetheus.Decred;
using Paymetheus.Framework;
using System.Windows.Input;

namespace Paymetheus.ViewModels
{
    public sealed class ShellViewModel : ShellViewModelBase
    {
        public ShellViewModel()
        {
            // Set the window title to the title in the resources assembly.
            // Append network name to window title if not running on mainnet.
            var activeNetwork = App.Current.ActiveNetwork;
            var productTitle = AssemblyResources.Title;
            if (activeNetwork == BlockChainIdentity.MainNet)
            {
                WindowTitle = productTitle;
            }
            else
            {
                WindowTitle = $"{productTitle} [{activeNetwork.Name}]";
            }

            CreateAccountCommand = new DelegateCommand(CreateAccount);

            StartupWizard = new StartupWizard(this);
            StartupWizardVisible = true;
        }

        private bool _startupWizardVisible;
        public bool StartupWizardVisible
        {
            get { return _startupWizardVisible; }
            set
            {
                if (_startupWizardVisible != value)
                {
                    _startupWizardVisible = value;
                    RaisePropertyChanged();
                }
            }
        }

        public StartupWizard StartupWizard { get; }

        public ICommand CreateAccountCommand { get; }

        private void CreateAccount()
        {
            VisibleDialogContent = new CreateAccountDialogViewModel(this);
        }
    }
}
