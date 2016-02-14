// Copyright (c) 2016 The btcsuite developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

namespace Paymetheus
{
    class WizardViewModelBase : ViewModelBase
    {
        public WizardViewModelBase(ShellViewModel shell) : base(shell) { }

        private WizardDialogViewModelBase _currentDialog;
        public WizardDialogViewModelBase CurrentDialog
        {
            get { return _currentDialog; }
            set { _currentDialog = value; RaisePropertyChanged(); }
        }
    }
}
