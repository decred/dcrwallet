// Copyright (c) 2016 The btcsuite developers
// Copyright (c) 2016 The Decred developers
// Licensed under the ISC license.  See LICENSE file in the project root for full license information.

namespace Paymetheus.Framework
{
    public class WizardViewModelBase : ViewModelBase
    {
        private WizardDialogViewModelBase _currentDialog;
        public WizardDialogViewModelBase CurrentDialog
        {
            get { return _currentDialog; }
            set { _currentDialog = value; RaisePropertyChanged(); }
        }
    }
}
